package apphost

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMigrateDropsGrants: a database that holds the grants table of an older
// node loses it on migrate, and keeps the tables apphost still reads.
func TestMigrateDropsGrants(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "apphost.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = gdb.Exec(`CREATE TABLE apphost__grants (id INTEGER PRIMARY KEY AUTOINCREMENT, identity TEXT)`).Error; err != nil {
		t.Fatal(err)
	}

	db := &DB{gdb}
	if err = db.migrate(); err != nil {
		t.Fatal(err)
	}

	if db.Migrator().HasTable("apphost__grants") {
		t.Fatal("apphost__grants survived the migration")
	}
	if !db.Migrator().HasTable(&dbAccessToken{}) || !db.Migrator().HasTable(&dbObjectHold{}) {
		t.Fatal("the migration lost a table apphost reads")
	}

	// why twice: every start migrates, and a database without the table must pass.
	if err = db.migrate(); err != nil {
		t.Fatal(err)
	}
}
