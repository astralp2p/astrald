package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// MigrateAgents brings the agent table to the current schema.
//
// why the rename runs first: the column carries the account holder's decision,
// and AutoMigrate would add `visible` beside the old `exposed` rather than move
// it, so every agent a node already holds would come back closed. Renaming
// carries the decision across; a node that never held the old column skips it.
func (db *DB) MigrateAgents() error {
	m := db.Migrator()
	if m.HasTable(&dbAgent{}) &&
		m.HasColumn(&dbAgent{}, "exposed") && !m.HasColumn(&dbAgent{}, "visible") {
		if err := m.RenameColumn(&dbAgent{}, "exposed", "visible"); err != nil {
			return err
		}
	}

	return db.AutoMigrate(&dbAgent{})
}

// CreateAgent inserts the agent row and stamps its creation time. The caller
// builds the row, so visibility lands in the same write as the rest of the record.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

func (db *DB) SetVisible(identity *astral.Identity, visible bool) error {
	tx := db.Model(&dbAgent{}).Where("identity = ?", identity).
		Update("visible", visible)
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
