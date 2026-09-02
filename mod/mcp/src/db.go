package mcp

import (
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// Migrate brings the module's tables to the current schema.
//
// why the message table is not AutoMigrated: it carries a generated column,
// three CHECKs and a partial index, none of which a struct tag expresses.
func (db *DB) Migrate() error {
	if err := db.AutoMigrate(&dbAgent{}); err != nil {
		return err
	}

	if err := db.Exec(ddlMessages).Error; err != nil {
		return err
	}
	for _, stmt := range ddlIndexes {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}

	return nil
}
