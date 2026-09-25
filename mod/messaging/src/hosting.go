package messaging

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// hosts answers whether this node hosts the identity's mailbox now: the index
// names an unexpired hosting contract for it, and auth finds that this node
// holds host_mailbox_action for it. Every eligibility decision asks this: the
// routing of a delivery or a receipt, every mail operation and mail method, and
// whether a sender is local when its message is fetched.
//
// why the index is asked first: it narrows the question to mailboxes this node
// provisioned, so a target it never hosted costs a map lookup and never reaches
// auth.
//
// why auth is asked as well: the index is derived state and the contract is the
// authority. A row outlives a contract that no longer authorizes, so the row
// alone would serve past the authority's end.
//
// why the node identity is refused by name: an anonymous local caller arrives
// as the node identity, and the root rule would let the node host a mailbox
// under its own identity.
//
// note: auth walks every active hosting contract naming this node, so one check
// costs more the more mailboxes this node hosts.
//
// note: only contracts this node provisioned are indexed, by create_identity
// and the legacy upgrade. A hosting contract auth indexed from elsewhere is not
// served until a provisioning path records it.
func (mod *Module) hosts(identity *astral.Identity) bool {
	if identity.IsZero() || identity.IsEqual(mod.node.Identity()) {
		return false
	}

	entry, ok := mod.mailboxes.Get(identity.String())
	if !ok || !entry.validAt(time.Now()) {
		return false
	}

	return mod.Auth.Authorize(mod.ctx, &messaging.HostMailboxAction{
		Action:    auth.NewAction(mod.node.Identity()),
		MailboxID: identity,
	})
}

// signHosting provisions the contract under which this node hosts the
// identity's mailbox, and answers the index entry it earns. The identity issues
// it with its own key, which the node holds, to this node: one permit for
// host_mailbox_action with no delegation, valid for Config.HostingDuration. The
// contract is signed, indexed with auth and stored.
//
// why a contract of its own and not a permit in the relay contract: relaying
// and hosting are separate authority, and neither grants the other.
//
// why Delegation 0: the node hosts the mailbox itself and hands the authority
// to nobody.
func (mod *Module) signHosting(ctx *astral.Context, identity *astral.Identity) (mailbox, error) {
	signed := &auth.SignedContract{Contract: &auth.Contract{
		Issuer:  identity,
		Subject: mod.node.Identity(),
		Permits: []*auth.Permit{{
			Action:     astral.String8(messaging.HostMailboxAction{}.ObjectType()),
			Delegation: 0,
		}},
		ExpiresAt: astral.Time(time.Now().Add(mod.config.HostingDuration)),
	}}

	if err := mod.Auth.SignContract(ctx, signed); err != nil {
		return mailbox{}, err
	}

	if err := mod.Auth.IndexContract(ctx, signed); err != nil {
		return mailbox{}, err
	}

	if _, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return mailbox{}, err
	}

	contractID, err := astral.ResolveObjectID(signed)
	if err != nil {
		return mailbox{}, err
	}

	expiresAt := signed.ExpiresAt.Time()

	return mailbox{ContractID: contractID, ExpiresAt: &expiresAt}, nil
}

// provisionPending provisions a hosting contract for every mailbox the legacy
// upgrade left pending, and records it. A row that cannot be provisioned — the
// node holds no key for it, or signing, indexing or storing fails — is logged
// and stays pending and unserved. A row deleted or provisioned while its
// contract was signed is logged as skipped.
//
// why a failure is logged and not returned: one mailbox that cannot be
// provisioned must not keep the others unserved, and Run has no caller to
// answer.
func (mod *Module) provisionPending(ctx *astral.Context) {
	rows, err := mod.db.ListPendingMailboxes()
	if err != nil {
		mod.log.Error("listing pending mailboxes: %v", err)
		return
	}

	for _, row := range rows {
		err = mod.provisionMailbox(ctx, row.Identity)
		switch {
		case errors.Is(err, errNotPending):
			mod.log.Logv(1, "skipped hosting of mailbox %v: deleted or provisioned meanwhile", row.Identity)
		case err != nil:
			mod.log.Error("mailbox %v stays pending and unserved: %v", row.Identity, err)
		default:
			mod.log.Logv(1, "provisioned hosting of mailbox %v", row.Identity)
		}
	}
}

// provisionMailbox provisions the hosting contract of one pending mailbox and
// records it on the row.
func (mod *Module) provisionMailbox(ctx *astral.Context, identity *astral.Identity) error {
	entry, err := mod.signHosting(ctx, identity)
	if err != nil {
		return err
	}

	return mod.recordHosting(identity, entry)
}
