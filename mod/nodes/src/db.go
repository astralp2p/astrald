package nodes

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func (db *DB) AddEndpoint(nodeID *astral.Identity, network, address string, expiresAt *time.Time) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}, {Name: "network"}, {Name: "address"}},
		DoUpdates: clause.AssignmentColumns([]string{"expires_at"}),
	}).Create(&dbEndpoint{
		Identity:  nodeID,
		Network:   network,
		Address:   address,
		ExpiresAt: expiresAt,
	}).Error
}

func (db *DB) RemoveEndpoint(nodeID *astral.Identity, network, address string) error {
	return db.Delete(&dbEndpoint{
		Identity: nodeID,
		Network:  network,
		Address:  address,
	}).Error
}

func (db *DB) FindEndpoints(nodeID *astral.Identity) (rows []*dbEndpoint, err error) {
	err = db.
		Where("identity = ?", nodeID).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		Find(&rows).Error

	return
}

func (db *DB) DeleteExpiredEndpoints(grace time.Duration) (int64, error) {
	tx := db.Where("expires_at IS NOT NULL AND expires_at <= ?", time.Now().Add(-grace).UTC()).
		Delete(&dbEndpoint{})
	return tx.RowsAffected, tx.Error
}

func (db *DB) HasEndpoints(nodeID *astral.Identity) (has bool) {
	db.
		Model(&dbEndpoint{}).
		Where("identity = ?", nodeID).
		Select("count(*) > 0").
		First(&has)
	return
}
