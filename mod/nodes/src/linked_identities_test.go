package nodes

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestLinkedIdentitiesListsEachLinkedPeerOnce: two links to one peer list it
// once, and a peer without a link is not listed.
func TestLinkedIdentitiesListsEachLinkedPeerOnce(t *testing.T) {
	mod := &Module{}
	mod.linkPool = NewLinkPool(mod)

	a, b, none := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	for _, id := range []*astral.Identity{a, a, b} {
		mod.linkPool.links.Add(&Link{id: astral.NewNonce(), conn: &identityConn{remote: id}})
	}

	got := mod.LinkedIdentities()
	if len(got) != 2 {
		t.Fatalf("LinkedIdentities = %v; want %v and %v once each", got, a, b)
	}

	seen := map[string]bool{}
	for _, id := range got {
		seen[id.String()] = true
	}
	if !seen[a.String()] || !seen[b.String()] || seen[none.String()] {
		t.Fatalf("LinkedIdentities = %v; want %v and %v", got, a, b)
	}
}
