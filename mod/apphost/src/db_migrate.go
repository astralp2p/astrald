package apphost

// migrate brings the module's tables to the current schema.
func (db *DB) migrate() error {
	return db.AutoMigrate(&dbAccessToken{}, &dbObjectHold{})
}
