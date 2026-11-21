package kcp

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

// identityNode is a node that only reports its identity.
//
// why: the embedded nil interface satisfies astral.Node; any other method
// panics, which asserts ResolveEndpoints reads nothing but the identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n identityNode) Identity() *astral.Identity { return n.id }

// staticIPs reports a fixed list of local IPs.
type staticIPs struct {
	ipmod.Module
	ips []ip.IP
}

func (s staticIPs) LocalIPs() ([]ip.IP, error) { return s.ips, nil }

// TestResolveEndpointsFollowsListen pins that the local identity resolves to
// kcp endpoints only while listen is unset or true, and that other identities
// resolve to nothing.
func TestResolveEndpointsFollowsListen(t *testing.T) {
	on, off := astral.Bool(true), astral.Bool(false)
	localIP, err := ip.ParseIP("192.0.2.1")
	if err != nil {
		t.Fatalf("ParseIP: %v", err)
	}

	local, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	var tests = []struct {
		name   string
		listen *astral.Bool
		nodeID *astral.Identity
		want   int
	}{
		{"local, listen unset", nil, local, 1},
		{"local, listen on", &on, local, 1},
		{"local, listen off", &off, local, 0},
		{"other, listen unset", nil, other, 0},
		{"other, listen on", &on, other, 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := astral.NewContext(nil)
			mod := &Module{
				node:     identityNode{id: local},
				config:   Config{ListenPort: 1791},
				settings: Settings{Listen: &tree.Value[*astral.Bool]{}},
			}
			mod.IP = staticIPs{ips: []ip.IP{localIP}}

			if test.listen != nil {
				if err := mod.settings.Listen.Set(ctx, test.listen); err != nil {
					t.Fatalf("set listen: %v", err)
				}
			}

			ch, err := mod.ResolveEndpoints(ctx, test.nodeID)
			if err != nil {
				t.Fatalf("ResolveEndpoints: %v", err)
			}

			var got []*kcp.Endpoint
			for e := range ch {
				got = append(got, e.Endpoint.(*kcp.Endpoint))
			}

			if len(got) != test.want {
				t.Fatalf("got %v endpoints, want %v", len(got), test.want)
			}
			for _, e := range got {
				if e.Port != 1791 {
					t.Errorf("endpoint port = %v, want 1791", e.Port)
				}
			}
		})
	}
}

// TestEndpointsTTLByAddressClass (criterion 1): a private, a CGNAT and a
// public interface address get LocalEndpointTTL, LocalEndpointTTL and 7 d.
func TestEndpointsTTLByAddressClass(t *testing.T) {
	mod := &Module{config: Config{ListenPort: 1792}}
	var ips []ip.IP
	for _, s := range []string{"192.168.1.20", "100.64.0.7", "203.0.113.5"} {
		a, err := ip.ParseIP(s)
		if err != nil {
			t.Fatalf("ParseIP(%v): %v", s, err)
		}
		ips = append(ips, a)
	}
	mod.IP = staticIPs{ips: ips}

	want := map[string]time.Duration{
		"192.168.1.20:1792": ipmod.LocalEndpointTTL,
		"100.64.0.7:1792":   ipmod.LocalEndpointTTL,
		"203.0.113.5:1792":  7 * 24 * time.Hour,
	}
	got := mod.endpoints()
	if len(got) != len(want) {
		t.Fatalf("endpoints() = %v; want %v rows", got, len(want))
	}
	for _, e := range got {
		ttl, ok := want[e.Address()]
		if !ok || e.TTL == nil || time.Duration(*e.TTL)*time.Second != ttl {
			t.Errorf("%v has TTL %v s; want %v", e.Address(), e.TTL, ttl)
		}
	}
}
