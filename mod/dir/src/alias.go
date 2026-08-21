package dir

import (
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/astral"
)

// SetAlias sets the alias for the identity. Set an empty alias to unset.
func (mod *Module) SetAlias(identity *astral.Identity, alias string) error {
	if alias == "" {
		return mod.db.Delete(&dbAlias{}, "identity = ?", identity).Error
	}

	return mod.db.Save(&dbAlias{
		Identity: identity,
		Alias:    alias,
	}).Error
}

// GetAlias returns the alias for the identity. Returns an empty string if no alias is set.
func (mod *Module) GetAlias(identity *astral.Identity) (string, error) {
	var row dbAlias
	if err := mod.db.First(&row, "identity = ?", identity).Error; err != nil {
		return "", err
	}

	return row.Alias, nil
}

// AliasMap returns a map of all aliases to identities.
func (mod *Module) AliasMap() *dir.AliasMap {
	var rows []dbAlias
	err := mod.db.Find(&rows).Error
	if err != nil {
		mod.log.Logv(1, "error loading alias map: %v", err)
		return nil
	}

	m := make(map[string]*astral.Identity, len(rows))
	for _, row := range rows {
		m[row.Alias] = row.Identity
	}

	return &dir.AliasMap{Aliases: m}
}
