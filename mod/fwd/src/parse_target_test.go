package fwd

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
)

// stubNode is an astral.Node with a fixed identity; routing is never exercised.
type stubNode struct {
	astral.Router
	id *astral.Identity
}

func (n stubNode) Identity() *astral.Identity { return n.id }

// stubDir resolves names from a fixed map; other dir.Module methods are not used.
type stubDir struct {
	dir.Module
	names map[string]*astral.Identity
}

func (d stubDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, ok := d.names[name]; ok {
		return id, nil
	}
	return nil, errors.New("unknown identity: " + name)
}

func (d stubDir) DisplayName(id *astral.Identity) string { return id.String() }

func TestParseTargetAstral(t *testing.T) {
	var node = astral.GenerateIdentity()
	var alice = astral.GenerateIdentity()
	var bob = astral.GenerateIdentity()

	mod := &Module{
		Deps: Deps{Dir: stubDir{names: map[string]*astral.Identity{"alice": alice, "bob": bob}}},
		node: stubNode{id: node},
	}

	tests := []struct {
		uri    string
		caller *astral.Identity
		target *astral.Identity
		query  string
	}{
		// forms without arguments
		{"astral://ssh", node, node, "ssh"},
		{"astral://bob:ssh", node, bob, "ssh"},
		{"astral://alice@ssh", alice, node, "ssh"},
		{"astral://alice@bob:ssh", alice, bob, "ssh"},
		// forms with arguments
		{"astral://svc?x=1", node, node, "svc?x=1"},
		{"astral://svc?addr=192.0.2.1:80", node, node, "svc?addr=192.0.2.1:80"},
		{"astral://svc?x=a@b", node, node, "svc?x=a@b"},
		{"astral://svc?from=a@b&to=tcp:1.2.3.4:80", node, node, "svc?from=a@b&to=tcp:1.2.3.4:80"},
		{"astral://bob:svc?addr=192.0.2.1:80", node, bob, "svc?addr=192.0.2.1:80"},
		{"astral://alice@svc?x=a@b", alice, node, "svc?x=a@b"},
		{"astral://alice@bob:svc?x=a@b:c", alice, bob, "svc?x=a@b:c"},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			router, err := mod.parseTarget(tt.uri)
			if err != nil {
				t.Fatalf("parseTarget(%q): %v", tt.uri, err)
			}

			q := router.(*AstralTarget).template
			if !q.Caller.IsEqual(tt.caller) {
				t.Errorf("caller = %v, want %v", q.Caller, tt.caller)
			}
			if !q.Target.IsEqual(tt.target) {
				t.Errorf("target = %v, want %v", q.Target, tt.target)
			}
			if string(q.QueryString) != tt.query {
				t.Errorf("query = %q, want %q", q.QueryString, tt.query)
			}
		})
	}
}
