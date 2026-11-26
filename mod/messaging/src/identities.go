package messaging

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/apphost"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	"gorm.io/gorm"
)

// CreateMailbox mints a new participant: a fresh identity with a signed relay
// contract, a signed hosting contract, an optional alias and an access token,
// and indexes its mailbox. A zero duration takes Config.TokenDuration.
//
// why the index row is written last: the row is what serves the mailbox, so a
// create that fails at any earlier step leaves no served mailbox behind.
func (mod *Module) CreateMailbox(ctx *astral.Context, alias string, duration astral.Duration) (*messaging.MailboxCredential, error) {
	identity, relay, err := mod.mintIdentity(ctx)
	if err != nil {
		return nil, err
	}

	hosting, err := mod.signHosting(ctx, identity)
	if err != nil {
		return nil, err
	}

	alias, err = mod.assignAlias(identity, alias)
	if err != nil {
		return nil, err
	}

	if duration == 0 {
		duration = astral.Duration(mod.config.TokenDuration)
	}

	token, err := mod.Apphost.CreateAccessToken(identity, duration)
	if err != nil {
		return nil, err
	}

	if err = mod.recordMailbox(identity, hosting); err != nil {
		return nil, err
	}

	// why: a sibling routes a delivery to the mailbox only through its relay-for
	// contract in the sibling's index, and the first-link sync has already run
	// for every linked sibling.
	// why after the index row: a create that fails pushes no contract of a
	// mailbox this node does not serve.
	// note: the push is best-effort; a sibling that misses it gets the contract
	// at its next first link or once a new link to it replaced a dead one.
	if mod.User != nil {
		mod.User.PushToSiblings(mod.ctx, relay)
	}

	mod.log.Logv(1, "created participant %v (%v)", alias, identity)

	return &messaging.MailboxCredential{
		Identity:  identity,
		Alias:     astral.String8(alias),
		Token:     token.Token,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// DeleteMailbox removes a participant: revokes every token its identity
// holds, unsets its alias and withdraws its mailbox from this node,
// with the mail it owns. An identity whose mailbox the index does not name
// answers messagingmod.ErrIdentityNotFound.
func (mod *Module) DeleteMailbox(_ *astral.Context, identity *astral.Identity) error {
	if err := mod.FindIdentity(identity); err != nil {
		return err
	}

	if err := mod.deleteMailbox(identity); err != nil {
		return err
	}

	mod.log.Logv(1, "deleted participant %v", identity)

	return nil
}

// FindIdentity answers nil for a participant: an identity whose mailbox the
// hosting index names, served or not. Any other identity answers
// messagingmod.ErrIdentityNotFound.
//
// why the table and not its mirror: a withdrawal whose table delete failed has
// dropped the mirror and kept the row, and DeleteMailbox finds that row again.
//
// why not hosts: a participant whose hosting contract expired is still one,
// which messaging.mailbox answers and messaging.delete_mailbox removes.
func (mod *Module) FindIdentity(identity *astral.Identity) error {
	if identity == nil || identity.IsZero() {
		return messagingmod.ErrIdentityNotFound
	}

	_, err := mod.db.FindMailbox(identity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return messagingmod.ErrIdentityNotFound
	}
	return err
}

// mintIdentity mints a fresh participant identity: a stored + indexed key and
// a signed node relay contract, mirroring apphost's register flow. Returns the
// identity and its relay contract.
func (mod *Module) mintIdentity(ctx *astral.Context) (*astral.Identity, *auth.SignedContract, error) {
	key := secp256k1.New()
	identity := secp256k1.Identity(secp256k1.PublicKey(key))

	contract, err := apphost.NewAppContract(identity, mod.node.Identity(), ContractDuration)
	if err != nil {
		return nil, nil, err
	}

	signed := &auth.SignedContract{Contract: contract}

	if signed.IssuerSig, err = secp256k1sig.SignASN1(key, contract.SignableHash()); err != nil {
		return nil, nil, err
	}

	if _, err = mod.Auth.SignSubject(ctx, signed); err != nil {
		return nil, nil, err
	}

	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return nil, nil, err
	}

	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), key); err != nil {
		return nil, nil, err
	}

	if err = mod.Crypto.AddToIndex(key); err != nil {
		return nil, nil, err
	}

	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return nil, nil, err
	}

	return identity, signed, nil
}

// assignAlias binds alias to the participant when one is given, and binds
// nothing when it is empty. Returns the alias actually set.
//
// why nothing is generated: an alias is node-global, so on a node holding many
// tenants' participants a generated name contends in a namespace none of them
// owns.
func (mod *Module) assignAlias(identity *astral.Identity, alias string) (string, error) {
	if alias == "" {
		return "", nil
	}

	if _, err := mod.Dir.ResolveIdentity(alias); err == nil {
		return "", errors.New("alias already taken")
	}

	return alias, mod.Dir.SetAlias(identity, alias)
}

// deleteMailbox revokes every access token the identity holds, unsets its alias and withdraws its mailbox, taking the mail it owns
// with it — both boxes, archived or not. A correspondent's own copy of the same
// message is owned by the correspondent and stays. The signed relay and hosting
// contracts stay indexed until they expire; apphost.withdraw_relay takes the relay
// contract back on this node.
//
// why every token and not the one create_mailbox issued: a token reissued
// through apphost authenticates the same identity, and one left standing keeps
// a deleted participant speaking to the node.
func (mod *Module) deleteMailbox(identity *astral.Identity) error {
	if err := mod.Apphost.DeleteAccessTokens(identity); err != nil {
		return err
	}

	if err := mod.Dir.SetAlias(identity, ""); err != nil {
		return err
	}

	return mod.withdrawMailbox(identity)
}
