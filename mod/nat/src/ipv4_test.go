package nat

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	ipmod "github.com/astralp2p/astrald/mod/ip"
	natmod "github.com/astralp2p/astrald/mod/nat"
)

// fixedIPs reports a fixed set of public IP candidates.
type fixedIPs struct {
	ipmod.Module
	ips []ip.IP
}

func (f *fixedIPs) PublicIPCandidates() []ip.IP { return f.ips }

func parseIPs(addrs ...string) []ip.IP {
	var out []ip.IP
	for _, a := range addrs {
		out = append(out, ip.IP(net.ParseIP(a)))
	}
	return out
}

// TestEvaluateEnabledRequiresPublicIPv4 holds that nat counts itself enabled,
// and is offered to peers, only when the IPv4-only puncher has an address.
func TestEvaluateEnabledRequiresPublicIPv4(t *testing.T) {
	cases := []struct {
		name    string
		ips     []ip.IP
		enabled bool
	}{
		{"no candidates", nil, false},
		{"IPv6 only", parseIPs("2001:4860:4860::8888"), false},
		{"IPv4 only", parseIPs("8.8.8.8"), true},
		{"IPv6 and IPv4", parseIPs("2001:4860:4860::8888", "8.8.8.8"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := &Module{
				Deps:     Deps{IP: &fixedIPs{ips: tc.ips}},
				settings: Settings{Enabled: &tree.Value[*astral.Bool]{}},
			}

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

// TestNodePunchAnswersNoSuitableIP holds that a participant without a public
// IPv4 address tells the initiator why the punch cannot proceed.
func TestNodePunchAnswersNoSuitableIP(t *testing.T) {
	mod := &Module{Deps: Deps{
		Auth: &recordingAuth{verdict: true},
		IP:   &fixedIPs{ips: parseIPs("2001:4860:4860::8888")},
	}}
	mod.enabled.Store(true)
	w := newRecordingWriter()

	if err := route(t, mod.OpNodePunch, astral.GenerateIdentity(), "nat.node_punch", w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}

	objs := w.objects(t)
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want one error_message", objs)
	}
	if got, want := objs[0].(astral.Error).Error(), natmod.ErrNoSuitableIP.Error(); got != want {
		t.Fatalf("error %q; want %q", got, want)
	}
}
