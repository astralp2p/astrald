package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// CreateAgent inserts the agent row and stamps its creation time. The caller
// builds the row, so exposure lands in the same write as the rest of the record.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

func (db *DB) SetExposed(identity *astral.Identity, exposed bool) error {
	tx := db.Model(&dbAgent{}).Where("identity = ?", identity).
		Update("exposed", exposed)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (db *DB) DeleteAgent(identity *astral.Identity) error {
	tx := db.Where("identity = ?", identity).Delete(&dbAgent{})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (db *DB) ListAgents() (list []dbAgent, _ error) {
	return list, db.Find(&list).Error
}
