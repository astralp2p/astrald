package nearby

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
)

func resolveEndpoints(t *testing.T, mod *Module, nodeID *astral.Identity) []*nodes.EndpointWithTTL {
	t.Helper()
	ch, err := mod.ResolveEndpoints(astral.NewContext(nil), nodeID)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}

	var list []*nodes.EndpointWithTTL
	timeout := time.After(waitTimeout)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return list
			}
			list = append(list, e)
		case <-timeout:
			t.Fatalf("ResolveEndpoints channel not closed after %d endpoints", len(list))
		}
	}
}

// A verified peer resolves to tcp at its source IP on 1791 and kcp on 1792, valid for the
// rest of its entry's life.
func TestResolveEndpointsOfAVerifiedPeer(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	h.clock.advance(2*time.Minute + 500*time.Millisecond)

	list := resolveEndpoints(t, h.mod, p.node)
	if len(list) != 2 {
		t.Fatalf("resolved %d endpoints; want 2", len(list))
	}

	tcpEndpoint, ok := list[0].Endpoint.(*tcp.Endpoint)
	if !ok || !bytes.Equal(tcpEndpoint.IP, ipOf(peerIP)) || tcpEndpoint.Port != 1791 {
		t.Fatalf("first endpoint %v; want tcp %v:1791", list[0].Endpoint, peerIP)
	}
	kcpEndpoint, ok := list[1].Endpoint.(*kcp.Endpoint)
	if !ok || !bytes.Equal(kcpEndpoint.IP, ipOf(peerIP)) || kcpEndpoint.Port != 1792 {
		t.Fatalf("second endpoint %v; want kcp %v:1792", list[1].Endpoint, peerIP)
	}

	remaining := statusTTL - 2*time.Minute - 500*time.Millisecond
	for _, e := range list {
		if e.TTL == nil || time.Duration(*e.TTL)*time.Second > remaining || time.Duration(*e.TTL)*time.Second < remaining-time.Second {
			t.Fatalf("TTL %v s; want the whole seconds of %v", e.TTL, remaining)
		}
	}
}

// No endpoint resolves for another node, an unrecognised stealth entry, an entry that
// names a node without verification, or an expired entry.
func TestResolveEndpointsOnlyForLiveVerifiedEntries(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)

	if err := h.mod.acceptStatus(ipOf("192.168.1.21"), p.stealth(t, h.clock.now(), astral.GenerateIdentity())); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if got := resolveEndpoints(t, h.mod, p.node); len(got) != 0 {
		t.Fatalf("an unrecognised entry resolved %d endpoints; want 0", len(got))
	}

	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "phone")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if got := resolveEndpoints(t, h.mod, astral.GenerateIdentity()); len(got) != 0 {
		t.Fatalf("another node resolved %d endpoints; want 0", len(got))
	}

	h.clock.advance(statusTTL + time.Second)
	if got := resolveEndpoints(t, h.mod, p.node); len(got) != 0 {
		t.Fatalf("an expired entry resolved %d endpoints; want 0", len(got))
	}
}

// ".name" resolves to the verified peer whose public profile names the alias.
func TestResolveIdentityByAlias(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "phone")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	got, err := h.mod.ResolveIdentity(".phone")
	if err != nil || !got.IsEqual(p.node) {
		t.Fatalf("ResolveIdentity(.phone) = %v, %v; want %v", got, err, p.node)
	}

	for _, s := range []string{"phone", ".tablet", "."} {
		if got, err := h.mod.ResolveIdentity(s); err == nil {
			t.Errorf("ResolveIdentity(%q) = %v; want an error", s, got)
		}
	}

	h.clock.advance(statusTTL + time.Second)
	if got, err := h.mod.ResolveIdentity(".phone"); err == nil {
		t.Errorf("ResolveIdentity(.phone) on an expired entry = %v; want an error", got)
	}
}

// An alias in an entry that is not verified resolves to nothing.
func TestResolveIdentityOnlyForVerifiedEntries(t *testing.T) {
	h := newHarness(t, nil)
	status := newPeer(t).visible(t, h.clock.now(), nil, "phone")
	if !h.mod.cache.storeUnrecognised(&entry{IP: ipOf(peerIP), Via: nearby.ViaProfile, Seen: h.clock.now(), Status: status}) {
		t.Fatal("cache refused the entry")
	}

	if got, err := h.mod.ResolveIdentity(".phone"); err == nil {
		t.Fatalf("ResolveIdentity(.phone) = %v; want an error for an unverified entry", got)
	}
}

// nearby.list streams one status per live entry: the verified node or nil, and the
// attachments as received, the proof included.
func TestOpListListsTheCache(t *testing.T) {
	h := newHarness(t, nil)
	h.mod.Auth = &recordingAuth{verdict: true}
	visible, hidden := newPeer(t), newPeer(t)

	if err := h.mod.acceptStatus(ipOf(peerIP), visible.visible(t, h.clock.now(), nil, "phone")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if err := h.mod.acceptStatus(ipOf("192.168.1.21"), hidden.stealth(t, h.clock.now(), astral.GenerateIdentity())); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	w := newRecordingWriter()
	if err := route(t, h.mod.OpList, astral.GenerateIdentity(), "nearby.list", w); err != nil {
		t.Fatalf("route: %v", err)
	}
	answers := w.answers(t)
	if len(answers) != 3 || answers[2].ObjectType() != (&astral.EOS{}).ObjectType() {
		t.Fatalf("answered %d objects; want 2 statuses and eos", len(answers))
	}

	var identified, unrecognised int
	for _, a := range answers[:2] {
		s, ok := a.(*nearby.Status)
		if !ok {
			t.Fatalf("answered %v; want a status", a.ObjectType())
		}
		if _, _, ok := nearby.SplitProof(s.Attachments.Objects()); !ok {
			t.Fatal("a listed status lost its proof")
		}
		switch {
		case s.Identity.IsEqual(visible.node):
			identified++
		case s.Identity.IsZero():
			unrecognised++
		}
	}
	if identified != 1 || unrecognised != 1 {
		t.Fatalf("listed %d identified and %d unrecognised statuses; want 1 and 1", identified, unrecognised)
	}
}
