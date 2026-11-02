package apphost

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
)

// legacyGrant is dbGrant as the replacing schema declared it: unique on
// (identity, name), and an ID the pure-Go driver creates without AUTOINCREMENT.
type legacyGrant struct {
	ID        uint             `gorm:"primaryKey;autoIncrement"`
	Identity  *astral.Identity `gorm:"uniqueIndex:idx_grants_identity_name"`
	Name      string           `gorm:"uniqueIndex:idx_grants_identity_name"`
	Data      []byte
	ExpiresAt *time.Time `gorm:"index"`
	CreatedAt time.Time
}

func (legacyGrant) TableName() string { return dbGrant{}.TableName() }

// openNodeDB opens a node database file the way a node does, through the
// sqlite driver the build selects.
func openNodeDB(t *testing.T) *DB {
	t.Helper()

	res, err := resources.NewFileResources(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	a, err := assets.NewCoreAssets(res)
	if err != nil {
		t.Fatalf("assets: %v", err)
	}
	return &DB{DB: a.Database()}
}

// openLegacyDB returns a node database whose grants table has the replacing
// schema and holds one grant for each of two actions.
func openLegacyDB(t *testing.T, app *astral.Identity) *DB {
	t.Helper()

	db := openNodeDB(t)
	if err := db.AutoMigrate(&legacyGrant{}); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	for _, permit := range []*auth.Permit{servePermit(auth.RoleDescriber), {Action: "mod.auth.see_objects_action"}} {
		row, err := fromGrantPermit(app, permit, nil)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		legacy := legacyGrant{Identity: row.Identity, Name: row.Name, Data: row.Data}
		if err := db.Create(&legacy).Error; err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
	}
	return db
}

// checkGrantsSchema fails t unless the grants table has the accumulating
// schema: no legacy index, the (identity, name, data) index, and AUTOINCREMENT.
func checkGrantsSchema(t *testing.T, db *DB) {
	t.Helper()

	if db.Migrator().HasIndex(&dbGrant{}, legacyGrantIndex) {
		t.Fatal("the legacy (identity, name) index survived the migration")
	}
	if !db.Migrator().HasIndex(&dbGrant{}, "idx_grants_identity_name_data") {
		t.Fatal("the (identity, name, data) index is missing")
	}

	var ddl string
	err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, dbGrant{}.TableName()).
		Scan(&ddl).Error
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
		t.Fatalf("the grants table has no AUTOINCREMENT: %v", ddl)
	}
}

// TestMigrateDropsTheLegacyIndexAndAddsAutoincrement: a node upgraded from the
// replacing schema keeps its grants and their IDs, accepts a second grant for
// an action it already grants, and never hands out a revoked grant's ID again.
func TestMigrateDropsTheLegacyIndexAndAddsAutoincrement(t *testing.T) {
	app := astral.GenerateIdentity()
	db := openLegacyDB(t, app)
	mod := &Module{db: db, log: log.New(nil)}
	before := grantIDs(t, mod, app)

	if err := db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	checkGrantsSchema(t, db)
	if after := grantIDs(t, mod, app); !slices.Equal(before, after) {
		t.Fatalf("grant IDs %v became %v", before, after)
	}

	if err := mod.Grant(app, servePermit(auth.RoleFinder), nil); err != nil {
		t.Fatalf("second grant for the action: %v", err)
	}
	ids := grantIDs(t, mod, app)
	if len(ids) != 3 {
		t.Fatalf("got %d rows; want 3", len(ids))
	}

	newest := ids[len(ids)-1]
	if err := mod.RevokeGrant(app, newest); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := mod.Grant(app, servePermit(auth.RoleSearcher), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ids = grantIDs(t, mod, app)
	if got := ids[len(ids)-1]; got <= newest {
		t.Fatalf("the new grant got ID %v; want above the revoked %v", got, newest)
	}
}

// TestMigrateIsIdempotent: every start runs the migration, and a second run
// keeps the schema and the rows, on a fresh node or an upgraded one.
func TestMigrateIsIdempotent(t *testing.T) {
	app := astral.GenerateIdentity()

	for name, db := range map[string]*DB{"fresh": openNodeDB(t), "legacy": openLegacyDB(t, app)} {
		mod := &Module{db: db, log: log.New(nil)}

		if err := db.migrate(); err != nil {
			t.Fatalf("%v: first migrate: %v", name, err)
		}
		ids := grantIDs(t, mod, app)

		if err := db.migrate(); err != nil {
			t.Fatalf("%v: second migrate: %v", name, err)
		}
		checkGrantsSchema(t, db)
		if again := grantIDs(t, mod, app); !slices.Equal(again, ids) {
			t.Fatalf("%v: the second migrate changed the rows: %v became %v", name, ids, again)
		}
	}
}

// TestGrantIDIsNotReusedAfterARestart: a start runs the migration again, and
// the ID of the newest grant, once revoked, still never comes back.
func TestGrantIDIsNotReusedAfterARestart(t *testing.T) {
	db := openNodeDB(t)
	mod := &Module{db: db, log: log.New(nil)}
	app := astral.GenerateIdentity()
	if err := db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder} {
		if err := mod.Grant(app, servePermit(role), nil); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	ids := grantIDs(t, mod, app)
	revoked := ids[len(ids)-1]
	if err := mod.RevokeGrant(app, revoked); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if err := db.migrate(); err != nil {
		t.Fatalf("migrate on restart: %v", err)
	}
	if err := mod.Grant(app, servePermit(auth.RoleSearcher), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ids = grantIDs(t, mod, app)
	if got := ids[len(ids)-1]; got <= revoked {
		t.Fatalf("the new grant got ID %v after a restart; want above the revoked %v", got, revoked)
	}
}
