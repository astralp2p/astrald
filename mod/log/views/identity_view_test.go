package views

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
)

type namingResolver struct {
	name string
}

func (r *namingResolver) ResolveIdentity(string) (*astral.Identity, error) { return nil, nil }

func (r *namingResolver) DisplayName(*astral.Identity) string { return r.name }

func setIdentityResolver(t *testing.T, r dir.Resolver) {
	t.Helper()
	saved := IdentityResolver.Get()
	t.Cleanup(func() { IdentityResolver.Set(saved) })
	IdentityResolver.Set(r)
}

func TestIdentityViewRender(t *testing.T) {
	id := astral.GenerateIdentity()

	t.Run("with a resolver", func(t *testing.T) {
		setIdentityResolver(t, &namingResolver{name: "alice"})
		if got := ansi.ReplaceAllString(IdentityView{Identity: id}.Render(), ""); got != "alice" {
			t.Fatalf("Render() = %q, want %q", got, "alice")
		}
	})

	t.Run("without a resolver", func(t *testing.T) {
		setIdentityResolver(t, nil)
		if got, want := ansi.ReplaceAllString(IdentityView{Identity: id}.Render(), ""), id.Fingerprint(); got != want {
			t.Fatalf("Render() = %q, want the fingerprint %q", got, want)
		}
	})
}

func TestObjectIDViewRender(t *testing.T) {
	oid := &astral.ObjectID{Size: 5, Hash: [32]byte{1, 2, 3}}

	if got, want := ansi.ReplaceAllString(ObjectIDView{ObjectID: oid}.Render(), ""), oid.String(); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}
