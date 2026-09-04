package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// CreateAgent inserts the agent row and stamps its creation time.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

// DeleteAgent removes the agent's row and every message it owns.
//
// why the mail goes with the row: a message is addressed to an identity this
// node holds an agent for, and nothing reaches one it does not. Left behind,
// the rows name an identity that has left agentIDs and whose row is gone, so no
// listing answers them and no read addresses them.
//
// why owner and not sender or recipient: owner is the recipient on an inbox row
// and the sender on an outbox row, so it names exactly the copies this agent
// holds. The other party's copy of the same message is owned by the other party
// and is theirs to keep — a deletion here is not a deletion from a
// correspondent's mailbox.
//
// why one transaction: the two writes are one act. A mail delete that commits
// without the row leaves an agent whose mail is gone, and a row delete that
// commits without the mail is the state this change exists to end.
func (db *DB) DeleteAgent(identity *astral.Identity) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("owner = ?", identity).Delete(&dbMessage{}).Error; err != nil {
			return err
		}

		res := tx.Where("identity = ?", identity).Delete(&dbAgent{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (db *DB) ListAgents() (list []dbAgent, _ error) {
	return list, db.Find(&list).Error
}
