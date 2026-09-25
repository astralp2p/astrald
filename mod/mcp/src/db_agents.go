package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// CreateAgent inserts the agent row and stamps its creation time.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now().UTC()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

// DeleteAgent removes the agent's row. The agent's mail is its messaging
// participant's, and goes with messaging.delete_identity.
func (db *DB) DeleteAgent(identity *astral.Identity) error {
	res := db.Where("identity = ?", identity).Delete(&dbAgent{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (db *DB) ListAgents() (list []dbAgent, _ error) {
	return list, db.Find(&list).Error
}
