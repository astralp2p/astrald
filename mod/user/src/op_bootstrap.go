package user

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/bip137sig"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// errClaimRefused answers a user the claim rule of auth.sign_contract refuses.
var errClaimRefused = errors.New("claim refused: the claim rule of auth.sign_contract refuses this user")

// errNoKeyDeriver answers user.bootstrap and user.derive_identity on a node
// without the bip137sig module.
var errNoKeyDeriver = errors.New("user key derivation requires the bip137sig module")

type opBootstrapArgs struct {
	Generate   bool
	Passphrase string
	In         string
	Out        string
}

// OpBootstrap claims this unclaimed node for the user that BIP-39 words derive
// along UserKeyPath, and returns an access token for that user. The words
// arrive in the stream, or the node generates them under Generate and returns
// them in the reply.
// Spec: .ai/system/protocols/user/ops/user.bootstrap.md
//
// why the origin and caller gates of auth.sign_contract: the op signs the
// contract that op signs for node setup, so it admits the same queries.
func (mod *Module) OpBootstrap(ctx *astral.Context, q *routing.IncomingQuery, args opBootstrapArgs) error {
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	if mod.ActiveContract() != nil {
		ch := q.Accept(channel.WithFormats(args.In, args.Out))
		defer ch.Close()
		return ch.Send(user.ErrAlreadyClaimed)
	}

	if !q.Caller().IsEqual(mod.node.Identity()) {
		return q.RejectWithCode(4)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	words, err := receiveWords(ch, args.Generate)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	userID, err := mod.bootstrap(ctx, q.Caller(), words, args.Passphrase)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: once the node is claimed, a session without a token reaches the
	// claimed anonymous_allowlist only, so the claiming app receives its
	// credential as the user here.
	token, err := mod.Apphost.CreateAccessToken(userID, apphostmod.DefaultTokenDuration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	result := &user.BootstrapResult{Identity: userID, Token: token.Token}
	if args.Generate {
		result.Mnemonic = astral.String16(strings.Join(words, " "))
	}

	return ch.Send(result)
}

// receiveWords reads the BIP-39 words from ch, or generates 24 words when
// generate is set.
func receiveWords(ch *channel.Channel, generate bool) ([]string, error) {
	if generate {
		entropy, err := bip137sig.NewEntropy(bip137sig.MaxEntropyBits)
		if err != nil {
			return nil, err
		}
		return bip137sig.EntropyToMnemonic(entropy)
	}

	var phrase *astral.String16
	err := ch.Switch(channel.Expect(&phrase), channel.FailOnEOF())
	if err != nil {
		return nil, err
	}

	return strings.Fields(string(*phrase)), nil
}

// bootstrap derives the user's key from words, signs the claim contract as the
// user and as the node, stores the user's key, and claims the node.
//
// why the key is stored before the claim: a repeat call with the same words
// completes a claim that failed after the store, while a claimed node without
// the user's key cannot sign user.adopt.
func (mod *Module) bootstrap(ctx *astral.Context, caller *astral.Identity, words []string, passphrase string) (*astral.Identity, error) {
	key, err := mod.deriveUserKey(words, passphrase)
	if err != nil {
		return nil, err
	}

	signed, err := mod.signClaim(ctx, caller, &key)
	if err != nil {
		return nil, err
	}

	_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), &key)
	if err != nil {
		return nil, err
	}

	err = mod.Crypto.AddToIndex(&key)
	if err != nil {
		return nil, err
	}

	err = mod.claim(ctx, signed)
	if err != nil {
		return nil, err
	}

	return signed.Issuer, nil
}

// deriveUserKey derives the user's key from BIP-39 words along UserKeyPath.
//
// why one function: user.derive_identity previews the user that user.bootstrap
// claims the node for, so both derive through it.
func (mod *Module) deriveUserKey(words []string, passphrase string) (crypto.PrivateKey, error) {
	if mod.Bip137sig == nil {
		return crypto.PrivateKey{}, errNoKeyDeriver
	}

	seed, err := bip137sig.MnemonicToSeed(words, passphrase)
	if err != nil {
		return crypto.PrivateKey{}, err
	}

	return mod.Bip137sig.DeriveKey(seed, usermod.UserKeyPath)
}

// signClaim builds the claim contract from the identity of key to this node,
// and signs it as the user with key in memory and as the node.
//
// why IsNodeClaim: the claim rule of auth.sign_contract decides whether this
// node may be claimed, and this op signs without passing through that op.
func (mod *Module) signClaim(ctx *astral.Context, caller *astral.Identity, key *crypto.PrivateKey) (*auth.SignedContract, error) {
	userID := secp256k1.Identity(secp256k1.PublicKey(key))

	contract, err := user.NewNodeContract(userID, mod.node.Identity(), true, usermod.DefaultContractValidity)
	if err != nil {
		return nil, err
	}

	if !mod.Auth.IsNodeClaim(ctx, caller, contract) {
		return nil, errClaimRefused
	}

	signed := &auth.SignedContract{Contract: contract}

	signed.IssuerSig, err = secp256k1sig.SignASN1(key, contract.SignableHash())
	if err != nil {
		return nil, err
	}

	_, err = mod.Auth.SignSubject(ctx, signed)
	if err != nil {
		return nil, err
	}

	return signed, mod.validateActiveContract(signed)
}
