package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dbEndpointHealth is what this node saw when it dialed one endpoint of an
// identity. The endpoints table holds what the subject claims; this table
// holds dial outcomes, so expiring or replacing a claim keeps its history.
type dbEndpointHealth struct {
	Identity *astral.Identity `gorm:"primaryKey"`
	Network  string           `gorm:"primaryKey"`
	Address  string           `gorm:"primaryKey"`
	// Scope is "" for a global record.
	// note: reserved for a dialer network fingerprint (FDglQbfCIC).
	Scope string `gorm:"primaryKey"`

	LastOK   *time.Time `gorm:"column:last_ok;index"`
	LastFail *time.Time `gorm:"column:last_fail"`
	OKCount  uint32     `gorm:"column:ok_count"`
	// FailStreak counts the failures since the last success.
	FailStreak uint32 `gorm:"column:fail_streak"`
	// LastLatency is the dial and handshake time of the last success.
	LastLatency time.Duration `gorm:"column:last_latency"`
	UpdatedAt   time.Time     `gorm:"column:updated_at;index"`
}

func (dbEndpointHealth) TableName() string {
	return nodes.DBPrefix + "endpoint_health"
}

// healthKey lists the primary key columns of dbEndpointHealth.
var healthKey = []clause.Column{{Name: "identity"}, {Name: "network"}, {Name: "address"}, {Name: "scope"}}

// RecordDialSuccess records a successful dial of one endpoint at at: it sets
// LastOK and LastLatency, counts the success and ends the failure streak.
func (db *DB) RecordDialSuccess(nodeID *astral.Identity, network, address string, at time.Time, latency time.Duration) error {
	return db.Clauses(clause.OnConflict{
		Columns: healthKey,
		DoUpdates: clause.Assignments(map[string]any{
			"last_ok":      at,
			"ok_count":     gorm.Expr("ok_count + 1"),
			"fail_streak":  0,
			"last_latency": latency,
			"updated_at":   at,
		}),
	}).Create(&dbEndpointHealth{
		Identity:    nodeID,
		Network:     network,
		Address:     address,
		LastOK:      &at,
		OKCount:     1,
		LastLatency: latency,
		UpdatedAt:   at,
	}).Error
}

// RecordDialFailure records a failed dial of one endpoint at at: it sets
// LastFail and extends the failure streak.
func (db *DB) RecordDialFailure(nodeID *astral.Identity, network, address string, at time.Time) error {
	return db.Clauses(clause.OnConflict{
		Columns: healthKey,
		DoUpdates: clause.Assignments(map[string]any{
			"last_fail":   at,
			"fail_streak": gorm.Expr("fail_streak + 1"),
			"updated_at":  at,
		}),
	}).Create(&dbEndpointHealth{
		Identity:   nodeID,
		Network:    network,
		Address:    address,
		LastFail:   &at,
		FailStreak: 1,
		UpdatedAt:  at,
	}).Error
}

// FindEndpointHealth returns the global health rows of nodeID.
func (db *DB) FindEndpointHealth(nodeID *astral.Identity) (rows []*dbEndpointHealth, err error) {
	err = db.Where("identity = ? AND scope = ?", nodeID, "").Find(&rows).Error
	return
}

// DeleteStaleEndpointHealth deletes the health rows last updated before before.
func (db *DB) DeleteStaleEndpointHealth(before time.Time) (int64, error) {
	tx := db.Where("updated_at < ?", before.UTC()).Delete(&dbEndpointHealth{})
	return tx.RowsAffected, tx.Error
}
