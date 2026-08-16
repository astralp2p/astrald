package crypto

import (
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// authorizeSigner reports whether caller may obtain a signature under signerKey.
// The rule is one sentence: sign only as the authenticated caller.
//
// why this guard exists: NewHashSigner and NewTextSigner resolve a key to a
// private key by public-key lookup alone (Module.PrivateKey), so the caller's
// identity plays no part in the resolution. The sign ops take a Key argument
// that overrides their default of the caller's own identity, which left every
// private key the node holds — the User's, the node's, any app's — signable by
// anyone the node answers. A signature is not authority confined to this node's
// runtime: it verifies on every node, for as long as the key lives, and it
// survives any later fix, so a forged membership contract, app contract or
// grant is genuine rather than merely accepted.
//
// why the node's own key is refused outright rather than allowed to its own
// identity: core.Router rewrites a query carrying no caller to the node's
// identity, so an unauthenticated local caller reaches an op wearing the node's
// identity and satisfies an equality check. apphost flags such a session with
// apphost.ExtraAnonymous precisely so an op can tell the two apart, but
// routing.Op builds the IncomingQuery from the query and its origin alone and
// drops Extra, so the flag cannot reach an op. Until it can, caller identity is
// not evidence that the node itself asked. Nothing internal is lost: in-process
// callers take Module.NodeSigner and never cross the op surface.
func (mod *Module) authorizeSigner(caller *astral.Identity, signerKey *crypto.PublicKey) error {
	signerID := secp256k1.Identity(signerKey)
	if signerID == nil {
		return cryptomod.ErrForeignKey
	}

	// checked before the equality rule below, which a promoted anonymous caller
	// would otherwise satisfy
	if signerID.IsEqual(mod.node.Identity()) {
		return cryptomod.ErrNodeKeyNotSignable
	}

	// a zero caller is never equal to a parsed key, so it refuses here
	if !signerID.IsEqual(caller) {
		return cryptomod.ErrForeignKey
	}

	return nil
}
