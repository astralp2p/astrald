package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
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

// AddManualEndpoint stores a row of nodeID that an operator added. It sets
// the expiry of an existing row and marks the row manual.
func (db *DB) AddManualEndpoint(nodeID *astral.Identity, network, address string, expiresAt *time.Time) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}, {Name: "network"}, {Name: "address"}},
		DoUpdates: clause.AssignmentColumns([]string{"expires_at", "manual"}),
	}).Create(&dbEndpoint{
		Identity:  nodeID,
		Network:   network,
		Address:   address,
		ExpiresAt: expiresAt,
		Manual:    true,
	}).Error
}

// StorePulledEndpoint stores a row of nodeID that a pull returned. A
// first-hand row sets the expiry of an existing row. A second-hand row, or
// any row for a manual one, only moves the expiry later.
// note: a nil expiresAt never expires.
func (db *DB) StorePulledEndpoint(nodeID *astral.Identity, endpoint endpointKey, expiresAt *time.Time, firstHand bool) error {
	keep := gorm.Expr("CASE WHEN (? OR manual) AND (expires_at IS NULL OR expires_at > excluded.expires_at) "+
		"THEN expires_at ELSE excluded.expires_at END", !firstHand)
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}, {Name: "network"}, {Name: "address"}},
		DoUpdates: clause.Assignments(map[string]any{"expires_at": keep}),
	}).Create(&dbEndpoint{
		Identity:  nodeID,
		Network:   endpoint.network,
		Address:   endpoint.address,
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

// ShortenEndpointExpiry moves the expiry of every pulled row of nodeID on
// network whose address is not in keep to until, unless the row expires
// earlier. A row without an expiry gets until. A manual row is never shortened.
func (db *DB) ShortenEndpointExpiry(nodeID *astral.Identity, network string, keep []string, until time.Time) (int64, error) {
	until = until.UTC()
	tx := db.Model(&dbEndpoint{}).
		Where("identity = ? AND network = ?", nodeID, network).
		Where("address NOT IN ?", keep).
		Where("manual = ?", false).
		Where("expires_at IS NULL OR expires_at > ?", until).
		Update("expires_at", until)
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
