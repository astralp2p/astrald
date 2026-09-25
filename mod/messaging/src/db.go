package messaging

import (
	"time"

	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

// Migrate brings the module's tables to the current schema, carrying a node's
// mail and agents over from the tables mod/mcp kept before this module existed.
//
// why the tables are not AutoMigrated: the message table carries a generated
// column, three CHECKs and a partial index, none of which a struct tag
// expresses, and the carry-over renames a table gorm would recreate.
//
// why one transaction: a node that stops halfway through would otherwise start
// next time with its mail under neither name or its agents' mailboxes indexed
// nowhere.
func (db *DB) Migrate() error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := migrateMailboxes(tx); err != nil {
			return err
		}
		if err := migrateLegacyMessages(tx); err != nil {
			return err
		}
		return execAll(tx, append([]string{ddlMessages}, ddlIndexes...))
	})
}

// migrateMailboxes creates the hosting index and, on the run that creates it,
// imports every agent mod/mcp holds as a pending mailbox. Run provisions the
// hosting contracts; a migration has no module to sign with.
//
// why the agents are copied only when the index is new: a later run would
// re-import an identity delete_identity removed while its mcp row stayed.
func migrateMailboxes(tx *gorm.DB) error {
	exists, err := hasTable(tx, tableMailboxes)
	if err != nil {
		return err
	}

	if err = execAll(tx, ddlMailboxes); err != nil || exists {
		return err
	}

	legacy, err := hasTable(tx, legacyAgents)
	if err != nil || !legacy {
		return err
	}

	return tx.Exec(copyLegacyAgents, time.Now().UTC()).Error
}

// migrateLegacyMessages renames the mail table mod/mcp kept, rows, cursor
// sequence and all, and drops the indexes named after it. The current indexes
// are created with the table afterwards.
//
// why a rename and not a copy: seq is the cursor every inbox pages by, and the
// rename keeps each row's seq and the AUTOINCREMENT high-water mark with it.
func migrateLegacyMessages(tx *gorm.DB) error {
	legacy, err := hasTable(tx, legacyMessages)
	if err != nil || !legacy {
		return err
	}

	current, err := hasTable(tx, tableMessages)
	if err != nil || current {
		return err
	}

	return execAll(tx, append([]string{renameLegacyMessages}, dropLegacyIndexes...))
}

// hasTable answers whether the database holds a table of this name.
func hasTable(tx *gorm.DB, name string) (bool, error) {
	var n int64
	err := tx.Raw(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).
		Scan(&n).Error
	return n > 0, err
}

// execAll runs each statement in order and stops at the first failure.
func execAll(tx *gorm.DB, statements []string) error {
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
