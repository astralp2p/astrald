package messaging

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/apphost"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
	"gorm.io/gorm"
)

// CreateIdentity mints a new participant: a fresh identity with a signed relay
// contract, a signed hosting contract, an optional alias and an access token,
// and indexes its mailbox. A zero duration takes Config.TokenDuration.
//
// why the index row is written last: the row is what serves the mailbox, so a
// create that fails at any earlier step leaves no served mailbox behind.
func (mod *Module) CreateIdentity(ctx *astral.Context, alias string, duration astral.Duration) (*messaging.IdentityCredential, error) {
	identity, err := mod.mintIdentity(ctx)
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

	mod.log.Logv(1, "created participant %v (%v)", alias, identity)

	return &messaging.IdentityCredential{
		Identity:  identity,
		Alias:     astral.String8(alias),
		Token:     token.Token,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// DeleteIdentity removes a participant: revokes every token and grant its
// identity holds, unsets its alias and withdraws its mailbox from this node,
// with the mail it owns. An identity whose mailbox the index does not name
// answers messagingmod.ErrIdentityNotFound.
func (mod *Module) DeleteIdentity(_ *astral.Context, identity *astral.Identity) error {
	if err := mod.FindIdentity(identity); err != nil {
		return err
	}

	if err := mod.deleteIdentity(identity); err != nil {
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
// dropped the mirror and kept the row, and DeleteIdentity finds that row again.
//
// why not hosts: a participant whose hosting contract expired is still one,
// which messaging.identity answers and messaging.delete_identity removes.
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
// a signed node relay contract, mirroring apphost's register flow.
func (mod *Module) mintIdentity(ctx *astral.Context) (*astral.Identity, error) {
	key := secp256k1.New()

	if _, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), key); err != nil {
		return nil, err
	}

	if err := mod.Crypto.AddToIndex(key); err != nil {
		return nil, err
	}

	identity := secp256k1.Identity(secp256k1.PublicKey(key))

	contract, err := apphost.NewAppContract(identity, mod.node.Identity(), ContractDuration)
	if err != nil {
		return nil, err
	}

	signed := &auth.SignedContract{Contract: contract}

	if err = mod.Auth.SignContract(ctx, signed); err != nil {
		return nil, err
	}

	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return nil, err
	}

	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return nil, err
	}

	return identity, nil
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

// deleteIdentity revokes every access token the identity holds, withdraws every
// grant, unsets its alias and withdraws its mailbox, taking the mail it owns
// with it — both boxes, archived or not. A correspondent's own copy of the same
// message is owned by the correspondent and stays. The signed relay and hosting
// contracts stay indexed until they expire.
//
// why every token and not the one create_identity issued: a token reissued
// through apphost authenticates the same identity, and one left standing keeps
// a deleted participant speaking to the node.
func (mod *Module) deleteIdentity(identity *astral.Identity) error {
	if err := mod.Apphost.DeleteAccessTokens(identity); err != nil {
		return err
	}

	if err := mod.revokeGrants(identity); err != nil {
		return err
	}

	if err := mod.Dir.SetAlias(identity, ""); err != nil {
		return err
	}

	return mod.withdrawMailbox(identity)
}

// revokeGrants withdraws every node-local grant identity holds. A grant names
// the identity and nothing else, so one left behind keeps authorizing whoever
// presents the deleted participant's identity.
//
// why every permit and not the unexpired ones: Grants reports expired rows too,
// and a row outlives the participant that held it. Deleting an expired grant
// takes away nothing the node was still honoring.
//
// why a failure stops the deletion: the index row is the only record naming
// the identity, so a run that removed the row and left a grant standing would
// leave authority messaging.delete_identity can no longer reach. The kept row
// makes the call repeatable.
func (mod *Module) revokeGrants(identity *astral.Identity) error {
	permits, err := mod.Apphost.Grants(identity)
	if err != nil {
		return err
	}

	for _, permit := range permits {
		// note: a concurrent revoke takes the row between the listing and here.
		err = mod.Apphost.Revoke(identity, string(permit.Action))
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return nil
}
