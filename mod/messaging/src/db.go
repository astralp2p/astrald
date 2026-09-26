package messaging

import (
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// Migrate creates the module's tables and indexes where they do not exist.
//
// why the tables are not AutoMigrated: the message table carries a generated
// column, three CHECKs and a partial index, none of which a struct tag
// expresses.
//
// why no transaction: every statement is IF NOT EXISTS, so a start that stops
// halfway completes the schema on the next one.
func (db *DB) Migrate() error {
	if err := execAll(db.DB, ddlMailboxes); err != nil {
		return err
	}
	return execAll(db.DB, append([]string{ddlMessages}, ddlIndexes...))
}

// execAll runs each statement in order and stops at the first failure.
func execAll(db *gorm.DB, statements []string) error {
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
