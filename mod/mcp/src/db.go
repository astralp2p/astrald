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
// why the old columns are dropped rather than left: the node holds no
// reachability, so a column that once carried it answers nothing and a reader
// finding one would take it for a decision something still makes.
func (db *DB) MigrateAgents() error {
	m := db.Migrator()

	if m.HasTable(&dbAgent{}) {
		for _, column := range []string{"visible", "exposed"} {
			if !m.HasColumn(&dbAgent{}, column) {
				continue
			}
			if err := m.DropColumn(&dbAgent{}, column); err != nil {
				return err
			}
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
