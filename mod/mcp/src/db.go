package mcp

import (
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// Migrate brings the module's tables to the current schema. Mail is the
// messaging module's, and so are its tables.
func (db *DB) Migrate() error {
	return db.AutoMigrate(&dbAgent{})
}
