package messaging

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
	"gorm.io/gorm"
)

// mailbox is the in-memory copy of one row of the hosting index. A nil
// ContractID is a pending row.
type mailbox struct {
	ContractID *astral.ObjectID
	ExpiresAt  *time.Time
}

// validAt answers whether the row names a hosting contract that has not expired
// at now. A pending row names none.
func (m mailbox) validAt(now time.Time) bool {
	return m.ContractID != nil && m.ExpiresAt != nil && now.Before(*m.ExpiresAt)
}

// loadMailboxes mirrors the hosting index into memory. It reads this module's
// own table and nothing else, so it runs at Load.
func (mod *Module) loadMailboxes() error {
	rows, err := mod.db.ListMailboxes()
	if err != nil {
		return err
	}

	for _, row := range rows {
		mod.mailboxes.Replace(row.Identity.String(), mailbox{
			ContractID: row.ContractID,
			ExpiresAt:  row.ExpiresAt,
		})
	}

	return nil
}

// recordMailbox writes the index row of a mailbox provisioned under entry and
// mirrors it.
//
// why the table first: a mirror ahead of a failed write would route on a
// decision nothing recorded.
func (mod *Module) recordMailbox(identity *astral.Identity, entry mailbox) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if err := mod.db.CreateMailbox(identity, entry.ContractID, *entry.ExpiresAt); err != nil {
		return err
	}

	mod.mailboxes.Replace(identity.String(), entry)

	return nil
}

// errNotPending is what recordHosting answers for a row deleted or provisioned
// in the meantime: it recorded nothing.
var errNotPending = errors.New("the mailbox is no longer pending")

// recordHosting records the contract a pending row was provisioned under and
// mirrors it. A row deleted or provisioned in the meantime is left as it is,
// and answers errNotPending.
func (mod *Module) recordHosting(identity *astral.Identity, entry mailbox) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	n, err := mod.db.SetMailboxContract(identity, entry.ContractID, *entry.ExpiresAt)
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotPending
	}

	mod.mailboxes.Replace(identity.String(), entry)

	return nil
}

// withdrawMailbox stops hosting the mailbox on this node: it drops the mirror,
// then deletes the index row with the mail the identity owns. An identity the
// index does not name answers messagingmod.ErrIdentityNotFound.
//
// why the hosting contract is left alone: this is a local withdrawal and not a
// revocation. The signed contract stays valid until its expiry wherever it is
// held, and this node stops hosting because its index no longer names the
// mailbox.
//
// why the mirror goes first: a delivery admitted after the table delete would
// store mail for a mailbox that is gone. A failed delete keeps the row, so the
// deletion can run again.
//
// why the write lock: a delivery or a send admitted before the withdrawal
// writes its row under the read lock, so it lands before the delete takes it
// or finds the mailbox gone — see whileIndexed.
func (mod *Module) withdrawMailbox(identity *astral.Identity) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	mod.mailboxes.Delete(identity.String())

	err := mod.db.DeleteMailbox(identity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return messagingmod.ErrIdentityNotFound
	}
	return err
}

// whileIndexed runs write while the index names the identity, and answers
// errNotParticipant without running it once the mailbox is withdrawn.
//
// why: hosting is checked when a request starts, and a delivery or a send
// writes its row later. A withdrawal in between would leave a row owned by an
// identity the index no longer names, which nothing lists and nothing deletes.
// The read lock keeps withdrawMailbox out from the check to the write.
//
// why the index and not hosts: authority was asked when the request started,
// and a request is not cut short by it — only a withdrawal deletes mail.
func (mod *Module) whileIndexed(identity *astral.Identity, write func() error) error {
	mod.mu.RLock()
	defer mod.mu.RUnlock()

	if _, ok := mod.mailboxes.Get(identity.String()); !ok {
		return errNotParticipant
	}
	return write()
}
