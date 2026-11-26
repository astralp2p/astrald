package nodes

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

// collectEndpoints drains ch into the "<network>:<address>" of each endpoint.
func collectEndpoints(ch <-chan *nodes.EndpointWithTTL) (keys []string) {
	for e := range ch {
		keys = append(keys, e.Network()+":"+e.Address())
	}
	return
}

// TestOwnIdentityResolvesOnlyListeners: a row stored under the node's own
// identity is never served, so the node resolves itself only to its listener
// endpoints, while another identity's row is still served.
// Spec: astral-docs protocols/nodes/ops/nodes.resolve_endpoints.md, Returned objects.
func TestOwnIdentityResolvesOnlyListeners(t *testing.T) {
	f := newExchangeFixture(t)
	f.mod.Exonet = &parsingExonet{}
	db := &DBEndpointResolver{mod: f.mod}
	f.mod.resolvers.Add(db)
	f.own.set(nodes.NewEndpointWithTTL(tcpEndpoint("10.77.0.12"), ipmod.LocalEndpointTTL))

	other := astral.GenerateIdentity()
	at := time.Now().Add(5 * time.Minute).UTC()
	for _, id := range []*astral.Identity{f.self, other} {
		if err := f.mod.db.AddEndpoint(id, "tcp", "10.77.0.99:1791", &at); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	ch, err := db.ResolveEndpoints(f.mod.ctx, f.self)
	if err != nil {
		t.Fatalf("DB ResolveEndpoints(self): %v", err)
	}
	if got := collectEndpoints(ch); len(got) != 0 {
		t.Fatalf("DB resolver served %v for the node itself; want nothing", got)
	}

	ch, err = f.mod.ResolveEndpoints(f.mod.ctx, f.self)
	if err != nil {
		t.Fatalf("ResolveEndpoints(self): %v", err)
	}
	if got := collectEndpoints(ch); len(got) != 1 || got[0] != "tcp:10.77.0.12:1791" {
		t.Fatalf("node resolved itself to %v; want only its listener tcp:10.77.0.12:1791", got)
	}

	ch, err = db.ResolveEndpoints(f.mod.ctx, other)
	if err != nil {
		t.Fatalf("DB ResolveEndpoints(other): %v", err)
	}
	if got := collectEndpoints(ch); len(got) != 1 || got[0] != "tcp:10.77.0.99:1791" {
		t.Fatalf("DB resolver served %v for another identity; want its stored row", got)
	}
}

// TestPullAboutSelfStoresNothing: a pull of the node's own endpoints from a
// sibling sends no query and stores no row; the same pull about another
// identity stores the sibling's answer.
func TestPullAboutSelfStoresNothing(t *testing.T) {
	f := newExchangeFixture(t)
	sibling := f.member(true)
	f.answer = endpointsWithTTL("10.77.0.12")

	n, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, sibling, f.self)
	if err != nil || n != 0 {
		t.Fatalf("UpdateNodeEndpoints(self) = %v, %v; want 0, nil", n, err)
	}
	select {
	case p := <-f.pulls:
		t.Fatalf("pulled %v from %v; want no query about the node itself", p.identity, p.resolver)
	default:
	}
	if rows, err := f.mod.db.FindEndpoints(f.self); err != nil || len(rows) != 0 {
		t.Fatalf("stored %v rows under the node's own identity (err %v); want none", len(rows), err)
	}

	other := astral.GenerateIdentity()
	if n, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, sibling, other); err != nil || n != 1 {
		t.Fatalf("UpdateNodeEndpoints(other) = %v, %v; want 1, nil", n, err)
	}
}
