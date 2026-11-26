package apphost

import "github.com/astralp2p/astrald/mod/apphost"

// droppedTables are the tables of node-local grants, which no authorization
// path reads any more: an app's permits come from its contract chain.
var droppedTables = []string{
	apphost.DBPrefix + "grants",
	apphost.DBPrefix + "grants_v1",
}

// migrate brings the module's tables to the current schema and drops the
// grant tables a node kept before.
func (db *DB) migrate() error {
	for _, table := range droppedTables {
		if err := db.Migrator().DropTable(table); err != nil {
			return err
		}
	}

	return db.AutoMigrate(&dbAccessToken{}, &dbObjectHold{})
}
