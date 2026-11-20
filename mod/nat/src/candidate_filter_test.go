package nat

import (
	"net"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	eventsmod "github.com/astralp2p/astrald/mod/events"
	ipmod "github.com/astralp2p/astrald/mod/ip"
	nodessrc "github.com/astralp2p/astrald/mod/nodes/src"
	tcpsrc "github.com/astralp2p/astrald/mod/tcp/src"
)

// ifaceIPs is an ip module whose interfaces hold a fixed set of addresses.
type ifaceIPs struct {
	ipmod.Module
	ips []ip.IP
}

func (i ifaceIPs) LocalIPs() ([]ip.IP, error) { return i.ips, nil }

// discardEvents drops emitted events.
type discardEvents struct{ eventsmod.Module }

func (discardEvents) Emit(data astral.Object) *events.Event { return &events.Event{Data: data} }

// orderedProviders is the ip module that lists its providers' candidates in a
// fixed order.
//
// why: the real ip module iterates an unordered set, and the bug under test
// depends on which address comes first.
type orderedProviders struct {
	ipmod.Module
	providers []ipmod.PublicIPCandidateProvider
}

func (o orderedProviders) PublicIPCandidates() (out []ip.IP) {
	for _, p := range o.providers {
		out = append(out, p.PublicIPCandidates()...)
	}
	return
}

// realFinders builds the two production candidate finders: the tcp module over
// the given interface addresses, and the nodes module holding the given
// observed endpoints. Neither is a stub of the filter.
func realFinders(ifaceAddrs, observed []string) []ipmod.PublicIPCandidateProvider {
	tcpMod := &tcpsrc.Module{Deps: tcpsrc.Deps{IP: ifaceIPs{ips: parseIPs(ifaceAddrs...)}}}
	nodesMod := &nodessrc.Module{Deps: nodessrc.Deps{Events: discardEvents{}}}

	for _, addr := range observed {
		ep := &tcp.Endpoint{IP: ip.IP(net.ParseIP(addr)), Port: 1791}
		nodesMod.AddObservedEndpoint(ep, ep.IP)
	}

	return []ipmod.PublicIPCandidateProvider{tcpMod, nodesMod}
}

func natOver(providers ...ipmod.PublicIPCandidateProvider) *Module {
	return &Module{
		Deps:     Deps{IP: orderedProviders{providers: providers}},
		settings: Settings{Enabled: &tree.Value[*astral.Bool]{}},
	}
}

// TestCarrierInternalInterfaceKeepsNatDisabled holds that a node whose only
// interface address is 464XLAT 192.0.0.8 does not enable nat, and that a real
// public address enables it.
func TestCarrierInternalInterfaceKeepsNatDisabled(t *testing.T) {
	cases := []struct {
		name    string
		iface   []string
		enabled bool
	}{
		{"only 192.0.0.8", []string{"192.0.0.8"}, false},
		{"only CGNAT 100.64.1.2", []string{"100.64.1.2"}, false},
		{"only IPv4-mapped CGNAT", []string{"::ffff:100.64.1.2"}, false},
		{"192.0.0.8 beside a LAN address", []string{"192.0.0.8", "192.168.1.2"}, false},
		{"only 31.61.247.94", []string{"31.61.247.94"}, true},
		{"192.0.0.8 beside 31.61.247.94", []string{"192.0.0.8", "31.61.247.94"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := natOver(realFinders(tc.iface, nil)...)

			mod.evaluateEnabled()

			if got := mod.enabled.Load(); got != tc.enabled {
				t.Fatalf("enabled=%v; want %v", got, tc.enabled)
			}
			if got := bool(mod.Evaluate(nil, "nat").Available); got != tc.enabled {
				t.Fatalf("advertised available=%v; want %v", got, tc.enabled)
			}
		})
	}
}

// TestGetLocalIPv4SkipsCarrierInternalAddress holds that the IP in the punch
// offer is the observed public address, whichever finder lists first.
func TestGetLocalIPv4SkipsCarrierInternalAddress(t *testing.T) {
	const want = "31.61.247.94"

	iface := realFinders([]string{"192.0.0.8"}, nil)[0]
	observed := realFinders(nil, []string{want})[1]

	for name, providers := range map[string][]ipmod.PublicIPCandidateProvider{
		"interface finder first": {iface, observed},
		"observed finder first":  {observed, iface},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := natOver(providers...).getLocalIPv4()
			if err != nil {
				t.Fatalf("getLocalIPv4: %v", err)
			}
			if got.String() != want {
				t.Fatalf("getLocalIPv4 = %v; want %s", got, want)
			}
		})
	}
}

// TestGetLocalIPv4WithOnlyCarrierInternalAddress holds that no punch IP exists
// until a peer reports the real one.
func TestGetLocalIPv4WithOnlyCarrierInternalAddress(t *testing.T) {
	mod := natOver(realFinders([]string{"192.0.0.8"}, nil)...)

	if got, err := mod.getLocalIPv4(); err == nil {
		t.Fatalf("getLocalIPv4 = %v; want an error", got)
	}

	if !slices.Equal(ipStrings(mod.IP.PublicIPCandidates()), nil) {
		t.Fatalf("candidates = %v; want none", mod.IP.PublicIPCandidates())
	}
}

func ipStrings(ips []ip.IP) (out []string) {
	for _, i := range ips {
		out = append(out, i.String())
	}
	return
}
