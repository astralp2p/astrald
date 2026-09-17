package crypto

import (
	"errors"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// note: any astral.Node method other than Identity panics on the nil embedded interface.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// note: any authmod.Module method other than Authorize panics on the nil embedded interface.
type recordingAuth struct {
	authmod.Module
	verdict bool

	mu      sync.Mutex
	actions []auth.ActionObject
}

func (a *recordingAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return a.verdict
}

func (a *recordingAuth) recorded() []auth.ActionObject {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auth.ActionObject(nil), a.actions...)
}

func newSignGuardModule(nodeID *astral.Identity, verdict bool) (*Module, *recordingAuth) {
	authority := &recordingAuth{verdict: verdict}
	mod := &Module{
		Deps: Deps{Auth: authority},
		node: &identityNode{id: nodeID},
	}
	return mod, authority
}

func TestAuthorizeSignerWithoutSudo(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	caller := astral.GenerateIdentity()

	tests := []struct {
		name   string
		caller *astral.Identity
		key    *crypto.PublicKey
		want   error
	}{
		{"malformed key", caller, &crypto.PublicKey{Type: secp256k1.KeyType, Key: []byte{1}}, cryptomod.ErrForeignKey},
		{"caller's own key", caller, secp256k1.FromIdentity(caller), nil},
		{"node key as the node", nodeID, secp256k1.FromIdentity(nodeID), cryptomod.ErrNodeKeyNotSignable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, authority := newSignGuardModule(nodeID, true)

			err := mod.authorizeSigner(astral.NewContext(nil), tt.caller, tt.key)
			if !errors.Is(err, tt.want) {
				t.Fatalf("authorizeSigner err = %v; want %v", err, tt.want)
			}

			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("authorizeSigner made %d authorization calls; want 0", n)
			}
		})
	}
}

func TestAuthorizeSignerForeignKeyAsksSudo(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	caller := astral.GenerateIdentity()
	signer := astral.GenerateIdentity()

	tests := []struct {
		name    string
		signer  *astral.Identity
		verdict bool
		want    error
	}{
		{"granted", signer, true, nil},
		{"refused", signer, false, cryptomod.ErrForeignKey},
		{"node key granted to another caller", nodeID, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, authority := newSignGuardModule(nodeID, tt.verdict)

			err := mod.authorizeSigner(astral.NewContext(nil), caller, secp256k1.FromIdentity(tt.signer))
			if !errors.Is(err, tt.want) {
				t.Fatalf("authorizeSigner err = %v; want %v", err, tt.want)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("authorizeSigner made %d authorization calls; want 1", len(actions))
			}

			sudo, ok := actions[0].(*authmod.SudoAction)
			if !ok {
				t.Fatalf("authorizeSigner asked about %T; want *authmod.SudoAction", actions[0])
			}

			if !sudo.Actor().IsEqual(caller) {
				t.Errorf("sudo actor = %v; want the caller %v", sudo.Actor(), caller)
			}

			if !sudo.AsID.IsEqual(tt.signer) {
				t.Errorf("sudo AsID = %v; want the signer %v", sudo.AsID, tt.signer)
			}
		})
	}
}
