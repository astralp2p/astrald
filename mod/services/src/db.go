package services

import (
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	db *gorm.DB
}

func (db *DB) Migrate() error {
	return db.db.AutoMigrate(
		&dbService{},
	)
}

// InTx executes fn inside a database transaction.
// This is the only transaction API - commit/rollback are fully encapsulated.
func (db *DB) InTx(fn func(tx *DB) error) error {
	return db.db.Transaction(func(txGorm *gorm.DB) error {
		return fn(&DB{db: txGorm})
	})
}

// deleteAllProviderServices deletes all cached services for a specific identity
func (db *DB) deleteAllProviderServices(providerID *astral.Identity) error {
	return db.db.
		Delete(&dbService{}, "provider_id = ?", providerID).
		Error
}

// deleteProviderService deletes a specific cached service for a specific identity
func (db *DB) deleteProviderService(providerID *astral.Identity, name string) error {
	return db.db.
		Delete(&dbService{}, "name = ? AND provider_id = ?", name, providerID).
		Error
}

// createProviderService creates a new service record
func (db *DB) createProviderService(providerID *astral.Identity, name string, info *astral.Bundle) error {
	return db.db.Create(&dbService{
		Name:       name,
		ProviderID: providerID,
		Info:       info,
	}).Error
}

// findProviderServices returns all current services for a specific identity
func (db *DB) findProviderServices(providerID *astral.Identity) ([]dbService, error) {
	var svcList []dbService
	err := db.db.
		Where("provider_id = ?", providerID).
		Order("created_at DESC").
		Find(&svcList).
		Error
	return svcList, err
}
