package apphost

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Prepare seeds fixed access tokens from the module config into the database.
// Each token is upserted (deleted then re-created) with a 100-year expiry so it
// persists across restarts without manual renewal.
func (mod *Module) Prepare(ctx context.Context) error {
	// load fixed access tokens from the config
	for token, name := range mod.config.Tokens {
		identity, err := mod.Dir.ResolveIdentity(name)
		if err != nil {
			mod.log.Error("config: cannot resolve identity '%v': %v", name, err)
			continue
		}

		err = mod.db.DeleteAccessToken(token)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			mod.log.Error("config: cannot delete token for '%v': %v", name, err)
			continue
		}

		err = mod.db.Create(&dbAccessToken{
			Identity:  identity,
			Token:     token,
			ExpiresAt: time.Now().Add(time.Hour * 24 * 365 * 100),
		}).Error
		if err != nil {
			mod.log.Error("config: cannot create token for '%v': %v", name, err)
		}
	}

	return nil
}
