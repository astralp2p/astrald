package crypto

import (
	"errors"
	"testing"

	authapi "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// identityNode is an astral.Node that answers Identity only.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// sudoTable answers SudoAction for the listed actor→target pairs and nothing
// else. Every other auth method panics through the embedded nil interface.
type sudoTable struct {
	authmod.Module
	pairs map[[2]string]bool
}

func (s *sudoTable) Authorize(_ *astral.Context, action authapi.ActionObject) bool {
	sudo, ok := action.(*authmod.SudoAction)
	return ok && s.pairs[[2]string{sudo.Actor().String(), sudo.AsID.String()}]
}

// TestAuthorizeSigner pins the signer rule every signing op shares: the
// caller's own key unless it is the node's, or a key the caller may sudo to.
func TestAuthorizeSigner(t *testing.T) {
	node := astral.GenerateIdentity()
	app := astral.GenerateIdentity()
	user := astral.GenerateIdentity()
	sudoer := astral.GenerateIdentity()

	mod := &Module{
		node: &identityNode{id: node},
		Deps: Deps{Auth: &sudoTable{pairs: map[[2]string]bool{
			{sudoer.String(), user.String()}: true,
			{sudoer.String(), node.String()}: true,
			// note: AuthorizeSudo admits an identity as itself, so the node
			// always holds sudo for itself; the self branch refuses first.
			{node.String(), node.String()}: true,
		}}},
	}

	for _, tc := range []struct {
		name   string
		caller *astral.Identity
		key    *crypto.PublicKey
		want   error
	}{
		{"self", app, secp256k1.FromIdentity(app), nil},
		{"the node as itself, holding sudo for itself", node, secp256k1.FromIdentity(node), cryptomod.ErrNodeKeyNotSignable},
		{"a foreign identity", app, secp256k1.FromIdentity(user), cryptomod.ErrForeignKey},
		{"the node key without sudo", app, secp256k1.FromIdentity(node), cryptomod.ErrForeignKey},
		{"sudo to a user", sudoer, secp256k1.FromIdentity(user), nil},
		{"sudo to the node", sudoer, secp256k1.FromIdentity(node), nil},
		{"the node as another identity", node, secp256k1.FromIdentity(user), cryptomod.ErrForeignKey},
		{"a key of another type", app, &crypto.PublicKey{Type: "ed25519", Key: []byte{1, 2, 3}}, cryptomod.ErrForeignKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mod.AuthorizeSigner(astral.NewContext(nil), tc.caller, tc.key)
			if !errors.Is(err, tc.want) {
				t.Fatalf("AuthorizeSigner = %v; want %v", err, tc.want)
			}
		})
	}
}
