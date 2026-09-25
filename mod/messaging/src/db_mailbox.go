package messaging

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// dbMailbox is one row of the hosting index: an identity whose mailbox this
// node was provisioned to host, and the hosting contract it holds for it. A nil
// ContractID is a pending row, and ExpiresAt is nil exactly when ContractID is.
// The alias is the directory's and the tokens are apphost's.
type dbMailbox struct {
	Identity   *astral.Identity
	ContractID *astral.ObjectID
	ExpiresAt  *time.Time
	CreatedAt  time.Time `gorm:"autoCreateTime:false;not null"`
}

func (dbMailbox) TableName() string {
	return tableMailboxes
}

// CreateMailbox inserts the index row for a mailbox provisioned under the
// hosting contract contractID, and stamps its creation time.
func (db *DB) CreateMailbox(identity *astral.Identity, contractID *astral.ObjectID, expiresAt time.Time) error {
	expiresAt = expiresAt.UTC()
	return db.Create(&dbMailbox{
		Identity:   identity,
		ContractID: contractID,
		ExpiresAt:  &expiresAt,
		CreatedAt:  time.Now().UTC(),
	}).Error
}

func (db *DB) FindMailbox(identity *astral.Identity) (row *dbMailbox, err error) {
	err = db.Where("identity = ?", identity).Take(&row).Error
	return
}

func (db *DB) ListMailboxes() (list []dbMailbox, _ error) {
	return list, db.Find(&list).Error
}

// ListPendingMailboxes answers the rows that name no hosting contract yet.
func (db *DB) ListPendingMailboxes() (list []dbMailbox, _ error) {
	return list, db.Where("contract_id IS NULL").Find(&list).Error
}

// SetMailboxContract records the hosting contract a pending row was provisioned
// under, and answers how many rows it changed: 0 when the row is gone or no
// longer pending.
//
// why only a pending row: a row that names a contract keeps it, so a second
// provisioning run changes nothing.
func (db *DB) SetMailboxContract(identity *astral.Identity, contractID *astral.ObjectID, expiresAt time.Time) (int64, error) {
	res := db.Model(&dbMailbox{}).
		Where("identity = ? AND contract_id IS NULL", identity).
		Updates(map[string]any{"contract_id": contractID, "expires_at": expiresAt.UTC()})
	return res.RowsAffected, res.Error
}

// DeleteMailbox removes the index row and every message the identity owns.
//
// why the mail goes with the row: a message is addressed to a mailbox this node
// hosts, and nothing reaches one it does not. Left behind, the rows name an
// identity whose row is gone, so no listing answers them and no read addresses
// them.
//
// why owner and not sender or recipient: owner is the recipient on an inbox row
// and the sender on an outbox row, so it names exactly the copies this
// participant holds. The other party's copy of the same message is owned by the
// other party and is theirs to keep — a deletion here is not a deletion from a
// correspondent's mailbox.
//
// why one transaction: the two writes are one act. A mail delete that commits
// without the row leaves a mailbox whose mail is gone, and a row delete that
// commits without the mail leaves mail no mailbox holds.
func (db *DB) DeleteMailbox(identity *astral.Identity) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("owner = ?", identity).Delete(&dbMessage{}).Error; err != nil {
			return err
		}

		res := tx.Where("identity = ?", identity).Delete(&dbMailbox{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
