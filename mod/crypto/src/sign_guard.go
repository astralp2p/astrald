package crypto

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// authorizeSigner reports whether caller may obtain a signature under signerKey.
// The rule is one sentence: sign as yourself, or as an identity you may sudo to.
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
// why SudoAction and not a signing-specific action: an Identity is a secp256k1
// public key, so signing under another party's key is that party acting, not a
// capability of its own. SudoAction already states that relation, mod/auth
// already resolves it through the contract chain, and mod/shell already gates
// its as= branch this way. Nothing new has to be defined for the grant path to
// exist: the issuer signs its holder a sudo permit and the chain terminates in
// AuthorizeSudo, which allows an identity to be itself.
//
// why the node's own key is refused on the self branch: core.Router rewrites a
// query with no caller to the node's identity, so an unauthenticated local
// caller arrives wearing it and satisfies the self test. apphost flags such a
// session (apphost.ExtraAnonymous) but routing.Op builds IncomingQuery from the
// query and its origin alone and drops Extra, so the flag cannot reach an op.
// The refusal is confined to the self branch on purpose — a caller holding a
// real sudo permit for the node still signs, because it proved something the
// router cannot fabricate. Internal callers take Module.NodeSigner and never
// pass through here.
func (mod *Module) authorizeSigner(ctx *astral.Context, caller *astral.Identity, signerKey *crypto.PublicKey) error {
	signerID := secp256k1.Identity(signerKey)
	if signerID == nil {
		return cryptomod.ErrForeignKey
	}

	if signerID.IsEqual(caller) {
		if signerID.IsEqual(mod.node.Identity()) {
			return cryptomod.ErrNodeKeyNotSignable
		}
		return nil
	}

	if mod.Auth.Authorize(ctx, &authmod.SudoAction{
		Action: auth.NewAction(caller),
		AsID:   signerID,
	}) {
		return nil
	}

	return cryptomod.ErrForeignKey
}
