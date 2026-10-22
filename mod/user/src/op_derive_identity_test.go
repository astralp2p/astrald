package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/bip137sig"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// routeDeriveIdentity sends one user.derive_identity query from the node with
// origin, as routeBootstrap does.
func routeDeriveIdentity(t *testing.T, f *bootstrapFixture, origin, queryString string, input astral.Object) ([]astral.Object, error) {
	t.Helper()
	return routeOp(t, f.mod.OpDeriveIdentity, query.New(f.nodeID, f.nodeID, queryString, nil), origin, input)
}

// onlyDerived fails unless the op answered exactly one DeriveIdentityResult.
func onlyDerived(t *testing.T, objs []astral.Object) *user.DeriveIdentityResult {
	t.Helper()
	if len(objs) != 1 {
		t.Fatalf("the op answered %d objects (%v); want one result", len(objs), objs)
	}
	result, ok := objs[0].(*user.DeriveIdentityResult)
	if !ok {
		t.Fatalf("the op answered %v; want a mod.user.derive_identity_result", objs[0])
	}
	return result
}

// TestDeriveIdentityWithGivenWords: the words answer the user they derive
// along m/1791'/0'/0'/0/0, the reply carries no words, and the node is
// unchanged.
func TestDeriveIdentityWithGivenWords(t *testing.T) {
	f := newBootstrapFixture()

	objs, err := routeDeriveIdentity(t, f, astral.OriginLocal, "user.derive_identity", astral.NewString16(bootstrapWords))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	result := onlyDerived(t, objs)

	if result.Identity.String() != bootstrapUser {
		t.Fatalf("identity %v; want %v", result.Identity, bootstrapUser)
	}
	if result.Mnemonic != "" {
		t.Fatal("the reply carries the words the caller sent")
	}
	checkUnchanged(t, f)
	if n := len(f.apphost.tokens()); n != 0 {
		t.Fatalf("the node issued %d access tokens; want none", n)
	}
}

// TestDeriveIdentityGeneratesWords: -generate answers 24 words and the user
// they derive, and the node is unchanged.
func TestDeriveIdentityGeneratesWords(t *testing.T) {
	f := newBootstrapFixture()

	objs, err := routeDeriveIdentity(t, f, "", "user.derive_identity?generate=true", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	result := onlyDerived(t, objs)

	if n := len(strings.Fields(string(result.Mnemonic))); n != 24 {
		t.Fatalf("the reply carries %d words; want 24", n)
	}
	if !identityOf(t, string(result.Mnemonic)).IsEqual(result.Identity) {
		t.Fatal("the returned words do not derive the returned identity")
	}
	checkUnchanged(t, f)
}

// TestDeriveIdentityPreviewsBootstrap: user.bootstrap claims the node for the
// user user.derive_identity answers for the same words and passphrase.
func TestDeriveIdentityPreviewsBootstrap(t *testing.T) {
	for _, passphrase := range []string{"", "correct horse"} {
		t.Run("passphrase "+passphrase, func(t *testing.T) {
			f := newBootstrapFixture()
			args := ""
			if passphrase != "" {
				args = "?passphrase=" + strings.ReplaceAll(passphrase, " ", "%20")
			}

			objs, err := routeDeriveIdentity(t, f, "", "user.derive_identity"+args, astral.NewString16(bootstrapWords))
			if err != nil {
				t.Fatalf("derive: %v", err)
			}
			previewed := onlyDerived(t, objs)
			if passphrase != "" && previewed.Identity.String() == bootstrapUser {
				t.Fatal("the passphrase did not reach the derivation")
			}

			objs, err = routeBootstrap(t, f, f.nodeID, "", "user.bootstrap"+args, astral.NewString16(bootstrapWords))
			if err != nil {
				t.Fatalf("bootstrap: %v", err)
			}
			claimed := onlyResult(t, objs)

			if !previewed.Identity.IsEqual(claimed.Identity) {
				t.Fatalf("previewed %v; claimed %v", previewed.Identity, claimed.Identity)
			}
			checkClaimed(t, f, previewed.Identity)
		})
	}
}

// TestDeriveIdentityRejectsNonLocalOrigins: a query of origin network or mcp is
// rejected before it is accepted.
func TestDeriveIdentityRejectsNonLocalOrigins(t *testing.T) {
	for _, origin := range []string{astral.OriginNetwork, astral.OriginMCP} {
		t.Run(origin, func(t *testing.T) {
			f := newBootstrapFixture()

			_, err := routeDeriveIdentity(t, f, origin, "user.derive_identity?generate=true", nil)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) || rejected.Code != astral.CodeRejected {
				t.Fatalf("got %v; want a rejection without a code", err)
			}
		})
	}
}

// TestDeriveIdentityRefusals: invalid words and a missing bip137sig module are
// answered with their error, and the node is unchanged.
func TestDeriveIdentityRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(f *bootstrapFixture)
		words   string
		want    string
	}{
		{"invalid checksum", func(*bootstrapFixture) {}, strings.Repeat("horse ", 18), bip137sig.ErrInvalidMnemonic.Error()},
		{"wrong word count", func(*bootstrapFixture) {}, "horse soldier", bip137sig.ErrInvalidMnemonicWordCount.Error()},
		{"no bip137sig module", func(f *bootstrapFixture) { f.mod.Bip137sig = nil }, bootstrapWords, errNoKeyDeriver.Error()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBootstrapFixture()
			c.prepare(f)

			objs, err := routeDeriveIdentity(t, f, "", "user.derive_identity", astral.NewString16(c.words))
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
				t.Fatalf("the op answered %v; want one error_message", objs)
			}
			if got := objs[0].(astral.Error).Error(); got != c.want {
				t.Fatalf("error %q; want %q", got, c.want)
			}
			checkUnchanged(t, f)
		})
	}
}
