package nearby

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
)

// endpointStrings returns the network and port of each endpoint, at peerIP.
func endpointStrings(t *testing.T, list []*nodes.EndpointWithTTL) []string {
	t.Helper()
	var out []string
	for _, e := range list {
		switch ep := e.Endpoint.(type) {
		case *tcp.Endpoint:
			if !bytes.Equal(ep.IP, ipOf(peerIP)) {
				t.Fatalf("tcp endpoint at %v; want %v", ep.IP, peerIP)
			}
			out = append(out, fmt.Sprintf("tcp:%d", ep.Port))
		case *kcp.Endpoint:
			if !bytes.Equal(ep.IP, ipOf(peerIP)) {
				t.Fatalf("kcp endpoint at %v; want %v", ep.IP, peerIP)
			}
			out = append(out, fmt.Sprintf("kcp:%d", ep.Port))
		default:
			t.Fatalf("resolved %T; want tcp or kcp", e.Endpoint)
		}
	}
	return out
}

// sealPorts returns ports sealed for node under the hint's nonce.
func sealPorts(t *testing.T, node *astral.Identity, hint *nearby.StealthHint, ports nearby.ListenPorts) *nearby.SealedPorts {
	t.Helper()
	sealed, err := nearby.SealPorts(node, hint.Nonce, &ports)
	if err != nil {
		t.Fatalf("SealPorts: %v", err)
	}
	return sealed
}

// A verified status resolves to the ports it carries, plaintext in Visible and sealed in
// Stealth, to 1791 and 1792 without ports, and to no endpoint for a port of 0. A port
// object the status cannot use leaves the defaults and the status accepted.
func TestResolveEndpointsOnTheStatusPorts(t *testing.T) {
	user := astral.GenerateIdentity()
	ports := nearby.ListenPorts{TCP: 1800, KCP: 1801}

	for _, tt := range []struct {
		name   string
		status func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage
		want   []string
	}{
		{"visible without ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.visible(t, h.clock.now(), user, "")
		}, []string{"tcp:1791", "kcp:1792"}},
		{"visible with listen ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: p.node, UserID: user}, &ports)
		}, []string{"tcp:1800", "kcp:1801"}},
		{"visible with tcp 1800 only", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: p.node, UserID: user},
				&nearby.ListenPorts{TCP: 1800})
		}, []string{"tcp:1800"}},
		{"visible with kcp 1801 only", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: p.node, UserID: user},
				&nearby.ListenPorts{KCP: 1801})
		}, []string{"kcp:1801"}},
		{"stealth without ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.stealth(t, h.clock.now(), user)
		}, []string{"tcp:1791", "kcp:1792"}},
		{"stealth with sealed ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			hint := stealthHint(t, p.node, user)
			return p.status(t, h.clock.now(), true, hint, sealPorts(t, p.node, hint, ports))
		}, []string{"tcp:1800", "kcp:1801"}},
		{"stealth with plaintext listen ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), true, stealthHint(t, p.node, user), &ports)
		}, []string{"tcp:1791", "kcp:1792"}},
		{"visible with sealed ports", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			hint := stealthHint(t, p.node, user)
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: p.node, UserID: user},
				sealPorts(t, p.node, hint, ports))
		}, []string{"tcp:1791", "kcp:1792"}},
		{"two port objects", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: p.node, UserID: user},
				&ports, &nearby.ListenPorts{TCP: 1900})
		}, []string{"tcp:1791", "kcp:1792"}},
		{"sealed ports of 3 bytes", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), true, stealthHint(t, p.node, user), &nearby.SealedPorts{Data: []byte{1, 2, 3}})
		}, []string{"tcp:1791", "kcp:1792"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, user)
			h.mod.recognizers.Add(userRecognizer(user))
			p := newPeer(t)

			if err := h.mod.acceptStatus(ipOf(peerIP), tt.status(t, h, p)); err != nil {
				t.Fatalf("acceptStatus: %v", err)
			}
			got := endpointStrings(t, resolveEndpoints(t, h.mod, p.node))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("resolved %v; want %v", got, tt.want)
			}
		})
	}
}

// An unrecognised stealth status with sealed ports is cached with its source IP only and
// resolves nothing.
func TestUnrecognisedStealthSealedPortsResolveNothing(t *testing.T) {
	h := newHarness(t, astral.GenerateIdentity())
	p := newPeer(t)
	hint := stealthHint(t, p.node, astral.GenerateIdentity())
	status := p.status(t, h.clock.now(), true, hint, sealPorts(t, p.node, hint, nearby.ListenPorts{TCP: 1800}))

	if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	e, ok := h.cached(peerIP)
	if !ok || e.NodeID != nil || e.Ports != nil {
		t.Fatalf("cached %+v (%v); want an IP-only entry without ports", e, ok)
	}
	if got := resolveEndpoints(t, h.mod, p.node); len(got) != 0 {
		t.Fatalf("resolved %d endpoints; want 0", len(got))
	}
}

// A port object changed after signing fails the proof, in both modes.
func TestAlteredPortsFailTheProof(t *testing.T) {
	user := astral.GenerateIdentity()
	signed := nearby.ListenPorts{TCP: 1800, KCP: 1801}
	sent := nearby.ListenPorts{TCP: 1900, KCP: 1801}

	for _, tt := range []struct {
		name   string
		status func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage
	}{
		{"visible", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			profile := &nearby.PublicProfile{NodeID: p.node, UserID: user}
			return statusOf(t, profile, &sent, p.proof(t, h.clock.now(), false, profile, &signed))
		}},
		{"stealth", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			hint := stealthHint(t, p.node, user)
			proof := p.proof(t, h.clock.now(), true, hint, sealPorts(t, p.node, hint, signed))
			return statusOf(t, hint, sealPorts(t, p.node, hint, sent), proof)
		}},
		{"stealth ports removed", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			hint := stealthHint(t, p.node, user)
			return statusOf(t, hint, p.proof(t, h.clock.now(), true, hint, sealPorts(t, p.node, hint, signed)))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, user)
			h.mod.recognizers.Add(userRecognizer(user))
			p := newPeer(t)

			if err := h.mod.acceptStatus(ipOf(peerIP), tt.status(t, h, p)); !errors.Is(err, errBadProof) {
				t.Fatalf("acceptStatus = %v; want %v", err, errBadProof)
			}
			h.requireNothingStored(t)
		})
	}
}
