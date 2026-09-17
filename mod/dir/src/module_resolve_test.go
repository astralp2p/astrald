package dir

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"gorm.io/gorm"
)

// note: any astral.Node method other than Identity panics on the nil embedded interface.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

type nameResolver struct {
	name string
	id   *astral.Identity
}

func (r *nameResolver) ResolveIdentity(s string) (*astral.Identity, error) {
	if s == r.name {
		return r.id, nil
	}
	return nil, errors.New("not found")
}

func (r *nameResolver) DisplayName(id *astral.Identity) string {
	if id.IsEqual(r.id) {
		return r.name
	}
	return ""
}

func newResolveDir(t *testing.T) (*Module, *astral.Identity) {
	t.Helper()

	nodeID := astral.GenerateIdentity()
	mod := newConfigureNodeStateDir(t, &recordingAuth{})
	mod.node = &identityNode{id: nodeID}
	mod.log = log.New(nil)

	return mod, nodeID
}

func mustSetAlias(t *testing.T, mod *Module, id *astral.Identity, alias string) {
	t.Helper()

	if err := mod.SetAlias(id, alias); err != nil {
		t.Fatalf("SetAlias(%v, %q): %v", id, alias, err)
	}
}

func TestResolveIdentityKeywordsAndHex(t *testing.T) {
	mod, nodeID := newResolveDir(t)

	for _, name := range []string{"", "anyone"} {
		got, err := mod.ResolveIdentity(name)
		if err != nil || !got.IsZero() {
			t.Errorf("ResolveIdentity(%q) = %v, %v; want the zero identity, nil", name, got, err)
		}
	}

	if got, err := mod.ResolveIdentity("localnode"); err != nil || !got.IsEqual(nodeID) {
		t.Errorf("ResolveIdentity(localnode) = %v, %v; want the node identity %v, nil", got, err, nodeID)
	}

	other := astral.GenerateIdentity()
	if got, err := mod.ResolveIdentity(other.String()); err != nil || !got.IsEqual(other) {
		t.Errorf("ResolveIdentity(hex) = %v, %v; want %v, nil", got, err, other)
	}
}

func TestResolveIdentityAliasThenResolvers(t *testing.T) {
	mod, nodeID := newResolveDir(t)
	mustSetAlias(t, mod, nodeID, "mynode")

	bob := astral.GenerateIdentity()
	if err := mod.AddResolver(&nameResolver{name: "bob", id: bob}); err != nil {
		t.Fatalf("AddResolver: %v", err)
	}

	if got, err := mod.ResolveIdentity("mynode"); err != nil || !got.IsEqual(nodeID) {
		t.Errorf("ResolveIdentity(mynode) = %v, %v; want the aliased node %v, nil", got, err, nodeID)
	}

	if got, err := mod.ResolveIdentity("bob"); err != nil || !got.IsEqual(bob) {
		t.Errorf("ResolveIdentity(bob) = %v, %v; want the resolver's %v, nil", got, err, bob)
	}

	const want = "unknown identity: nobody"
	if got, err := mod.ResolveIdentity("nobody"); err == nil || err.Error() != want {
		t.Errorf("ResolveIdentity(nobody) = %v, %v; want error %q", got, err, want)
	}
}

func TestDisplayName(t *testing.T) {
	mod, nodeID := newResolveDir(t)
	mustSetAlias(t, mod, nodeID, "mynode")

	bob := astral.GenerateIdentity()
	if err := mod.AddResolver(&nameResolver{name: "bob", id: bob}); err != nil {
		t.Fatalf("AddResolver: %v", err)
	}

	unknown := astral.GenerateIdentity()

	tests := []struct {
		name string
		id   *astral.Identity
		want string
	}{
		{"nil", nil, ZeroIdentity},
		{"zero", &astral.Identity{}, ZeroIdentity},
		{"aliased", nodeID, "mynode"},
		{"resolver named", bob, "bob"},
		{"unknown", unknown, unknown.Fingerprint()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mod.DisplayName(tt.id); got != tt.want {
				t.Fatalf("DisplayName = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestApplyFilters(t *testing.T) {
	mod := &Module{}
	mod.SetFilter("f", func(*astral.Identity) bool { return false })
	mod.SetFilter("g", func(*astral.Identity) bool { return true })

	id := astral.GenerateIdentity()

	tests := []struct {
		name    string
		filters []string
		want    bool
	}{
		{"no filters", nil, false},
		{"unknown filter", []string{"missing"}, false},
		{"false only", []string{"f"}, false},
		{"any filter passes", []string{"f", "g"}, true},
		{"unknown filter skipped", []string{"missing", "g"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mod.ApplyFilters(id, tt.filters...); got != tt.want {
				t.Fatalf("ApplyFilters(%v) = %v; want %v", tt.filters, got, tt.want)
			}
		})
	}
}

func TestSetAliasUniqueAndDelete(t *testing.T) {
	mod, nodeID := newResolveDir(t)
	mustSetAlias(t, mod, nodeID, "mynode")

	other := astral.GenerateIdentity()
	if err := mod.SetAlias(other, "mynode"); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint") {
		t.Fatalf("SetAlias(other, mynode) err = %v; want a UNIQUE constraint error", err)
	}

	if got, err := mod.GetAlias(nodeID); err != nil || got != "mynode" {
		t.Fatalf("GetAlias(node) after refused duplicate = %q, %v; want mynode, nil", got, err)
	}

	mustSetAlias(t, mod, nodeID, "")

	if got, err := mod.GetAlias(nodeID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetAlias after delete = %q, %v; want %v", got, err, gorm.ErrRecordNotFound)
	}
}

func TestSetDefaultAliasGeneratesWhenMissing(t *testing.T) {
	mod, nodeID := newResolveDir(t)

	if err := mod.setDefaultAlias(); err != nil {
		t.Fatalf("setDefaultAlias err = %v; want nil", err)
	}

	if got, err := mod.GetAlias(nodeID); err != nil || got == "" {
		t.Fatalf("GetAlias(node) = %q, %v; want a generated alias, nil", got, err)
	}
}

func TestSetDefaultAliasKeepsExisting(t *testing.T) {
	mod, nodeID := newResolveDir(t)
	mustSetAlias(t, mod, nodeID, "mynode")

	if err := mod.setDefaultAlias(); err != nil {
		t.Fatalf("setDefaultAlias err = %v; want nil", err)
	}

	if got, err := mod.GetAlias(nodeID); err != nil || got != "mynode" {
		t.Fatalf("GetAlias(node) = %q, %v; want mynode, nil", got, err)
	}
}

func TestAliasMap(t *testing.T) {
	mod, nodeID := newResolveDir(t)
	other := astral.GenerateIdentity()
	mustSetAlias(t, mod, nodeID, "mynode")
	mustSetAlias(t, mod, other, "other")

	m := mod.AliasMap()
	if m == nil {
		t.Fatal("AliasMap = nil; want a map")
	}

	if len(m.Aliases) != 2 {
		t.Fatalf("AliasMap has %d entries; want 2: %v", len(m.Aliases), m.Aliases)
	}

	want := map[string]*astral.Identity{"mynode": nodeID, "other": other}
	for alias, id := range want {
		if got := m.Aliases[alias]; !got.IsEqual(id) {
			t.Errorf("AliasMap[%q] = %v; want %v", alias, got, id)
		}
	}
}
