package gateway

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// namingDir resolves names in the order mod/dir/src/module.go does: the empty
// name and "anyone" to the zero identity, then a hex identity, then an alias.
// Every other method panics.
type namingDir struct {
	dirmod.Module
	aliases map[string]*astral.Identity
}

func (d *namingDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if name == "" || name == "anyone" {
		return &astral.Identity{}, nil
	}
	if id, err := astral.ParseIdentity(name); err == nil {
		return id, nil
	}
	if id, ok := d.aliases[name]; ok {
		return id, nil
	}
	return nil, errors.New("unknown identity: " + name)
}

// TestNodeRouteForwardsTheResolvedIdentity: a name is resolved on this node,
// and the next hop receives the identity in hex, never the name.
func TestNodeRouteForwardsTheResolvedIdentity(t *testing.T) {
	f := newUseGatewayFixture(t, true, &recordingAuth{verdict: true})

	w := newRecordingWriter()
	if err := route(t, f.mod.OpNodeRoute, astral.GenerateIdentity(), "gateway.node_route?identity=peer", w); err != nil {
		t.Fatalf("gateway.node_route refused an authorized caller: %v", err)
	}
	answered(t, w)

	assertUseGatewayForwarded(t, f)

	_, params := query.Parse(f.node.queries()[0].QueryString)
	if len(params) != 1 || params["identity"] != f.target.String() {
		t.Fatalf("forwarding sent arguments %v; want only identity=%v", params, f.target)
	}
}

// TestNodeRouteRefusesAnUnusableIdentity: the zero identity and an unresolved
// name are refused before authorization, and nothing is routed onward.
func TestNodeRouteRefusesAnUnusableIdentity(t *testing.T) {
	for _, name := range []string{"anyone", "nobody"} {
		t.Run("identity="+name, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			f := newUseGatewayFixture(t, true, authority)
			w := newRecordingWriter()

			err := route(t, f.mod.OpNodeRoute, astral.GenerateIdentity(), "gateway.node_route?identity="+name, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("gateway.node_route answered identity=%q: got err %v, want a rejection", name, err)
			}
			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("gateway.node_route made %d authorization calls; want none", n)
			}
			if n := len(f.node.queries()); n != 0 {
				t.Fatalf("gateway.node_route routed %d queries onward; want none", n)
			}
		})
	}
}

// TestNodeConnectResolvesAnAlias: -identity takes a name, and the reservation
// is made for the node the name resolves to.
func TestNodeConnectResolvesAnAlias(t *testing.T) {
	f := newUseGatewayFixture(t, true, &recordingAuth{verdict: true})
	w := newRecordingWriter()

	if err := route(t, f.mod.OpNodeConnect, astral.GenerateIdentity(), "gateway.node_connect?identity=peer", w); err != nil {
		t.Fatalf("gateway.node_connect refused an authorized caller: %v", err)
	}
	answered(t, w)

	if !f.claimed() || len(f.mod.connectors.Clone()) != 1 {
		t.Fatal("gateway.node_connect did not reserve the aliased node's idle connection")
	}
}
