package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

func (db *DB) CreateAgent(identity *astral.Identity, alias, token string, expiresAt time.Time) error {
	return db.Create(&dbAgent{
		Identity:  identity,
		Alias:     alias,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}).Error
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
