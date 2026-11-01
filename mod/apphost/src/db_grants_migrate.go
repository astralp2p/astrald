package apphost

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// legacyGrantsTable holds the grants table's rows while it is rebuilt.
const legacyGrantsTable = "apphost__grants_v1"

// migrate brings the module's tables to the current schema.
//
// why the grants sequence is read and written back: the pure-Go driver reports
// every column as NOT NULL, so AutoMigrate rebuilds the grants table on every
// start, and dropping the old table drops its AUTOINCREMENT high-water mark.
// Without it a revoked grant's ID is handed out again after a restart.
func (db *DB) migrate() error {
	if err := db.migrateGrants(); err != nil {
		return err
	}

	seq, err := db.grantsSequence()
	if err != nil {
		return err
	}

	if err := db.AutoMigrate(&dbAccessToken{}, &dbObjectHold{}, &dbGrant{}); err != nil {
		return err
	}

	return db.setGrantsSequence(seq)
}

// grantsSequence returns the highest grant ID ever handed out, or 0 when none
// was.
//
// note: a grants table that exists here has AUTOINCREMENT, so sqlite_sequence
// exists too.
func (db *DB) grantsSequence() (seq uint64, err error) {
	if !db.Migrator().HasTable(&dbGrant{}) {
		return 0, nil
	}

	err = db.Raw(`SELECT COALESCE(MAX(seq), 0) FROM sqlite_sequence WHERE name = ?`, dbGrant{}.TableName()).
		Scan(&seq).Error
	return seq, err
}

// setGrantsSequence records seq as the highest grant ID ever handed out. A zero
// seq records nothing.
func (db *DB) setGrantsSequence(seq uint64) error {
	if seq == 0 {
		return nil
	}

	table := dbGrant{}.TableName()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM sqlite_sequence WHERE name = ?`, table).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)`, table, seq).Error
	})
}

// migrateGrants turns a grants table of the replacing schema into one that
// accumulates. A table already migrated is left as it is.
//
// why the legacy index is dropped by hand: AutoMigrate creates the new unique
// index but leaves the old one, which would still refuse a second grant for an
// action.
//
// why the table is rebuilt: AutoMigrate does not add AUTOINCREMENT to an
// existing table, and the pure-Go driver created it without one.
func (db *DB) migrateGrants() error {
	m := db.Migrator()
	if !m.HasTable(&dbGrant{}) {
		return nil
	}

	if m.HasIndex(&dbGrant{}, legacyGrantIndex) {
		if err := m.DropIndex(&dbGrant{}, legacyGrantIndex); err != nil {
			return err
		}
	}

	var ddl string
	err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, dbGrant{}.TableName()).
		Scan(&ddl).Error
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
		return nil
	}

	return db.Transaction(rebuildGrants)
}

// rebuildGrants recreates the grants table from the current model, keeping
// every row and its ID.
//
// why the expiry index is dropped first: index names are global and a renamed
// table keeps its indexes, so the new table could not create it.
func rebuildGrants(tx *gorm.DB) error {
	table := dbGrant{}.TableName()

	if err := tx.Exec(`DROP INDEX IF EXISTS ?`, clause.Table{Name: grantsExpiryIndex}).Error; err != nil {
		return err
	}
	if err := tx.Migrator().RenameTable(table, legacyGrantsTable); err != nil {
		return err
	}
	if err := tx.AutoMigrate(&dbGrant{}); err != nil {
		return err
	}

	err := tx.Exec(`INSERT INTO ? (id, identity, name, data, expires_at, created_at) `+
		`SELECT id, identity, name, data, expires_at, created_at FROM ?`,
		clause.Table{Name: table}, clause.Table{Name: legacyGrantsTable}).Error
	if err != nil {
		return err
	}

	return tx.Migrator().DropTable(legacyGrantsTable)
}
