package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dbEndpointHealth is what this node knows of one endpoint of an identity:
// how consistently the subject lists it, and how this node's dials of it went.
// The endpoints table holds what the subject claims at the moment; this table
// holds history, so expiring or replacing a claim keeps it.
type dbEndpointHealth struct {
	Identity *astral.Identity `gorm:"primaryKey"`
	Network  string           `gorm:"primaryKey"`
	Address  string           `gorm:"primaryKey"`
	// Scope is "" for a global record, else the network fingerprint of the dialer.
	// note: the claim fields are always written under Scope "", the dial fields
	// of a non-public tcp or kcp endpoint under the fingerprint; see scopeOf.
	Scope string `gorm:"primaryKey"`

	// FirstSeen and LastSeen bracket the first-hand replies that listed the endpoint.
	FirstSeen *time.Time `gorm:"column:first_seen"`
	LastSeen  *time.Time `gorm:"column:last_seen"`
	// SeenDays counts the UTC days with a first-hand reply that listed the endpoint.
	SeenDays uint32 `gorm:"column:seen_days"`

	LastOK   *time.Time `gorm:"column:last_ok;index"`
	LastFail *time.Time `gorm:"column:last_fail"`
	// FailStreak counts the failures since the last success.
	FailStreak uint32 `gorm:"column:fail_streak"`
	// SuccessEWMA is the decaying success rate of the dials: 1 per success, 0 per failure.
	// It starts from successPrior; it is meaningful only once a dial is recorded.
	SuccessEWMA float64 `gorm:"column:success_ewma"`
	// LatencyEWMA is the decaying dial and handshake time of the successes.
	LatencyEWMA time.Duration `gorm:"column:latency_ewma"`
	UpdatedAt   time.Time     `gorm:"column:updated_at;index"`
}

func (dbEndpointHealth) TableName() string {
	return nodes.DBPrefix + "endpoint_health"
}

// hasDialHistory reports whether this node recorded a dial of the row's endpoint.
func (h *dbEndpointHealth) hasDialHistory() bool {
	return h != nil && (h.LastOK != nil || h.LastFail != nil)
}

// healthKey lists the primary key columns of dbEndpointHealth.
var healthKey = []clause.Column{{Name: "identity"}, {Name: "network"}, {Name: "address"}, {Name: "scope"}}

// healthRef names one health row.
type healthRef struct {
	identity *astral.Identity
	endpoint endpointKey
	scope    string
}

// newRow returns an empty row at ref updated at at.
func (r healthRef) newRow(at time.Time) *dbEndpointHealth {
	return &dbEndpointHealth{
		Identity:  r.identity,
		Network:   r.endpoint.network,
		Address:   r.endpoint.address,
		Scope:     r.scope,
		UpdatedAt: at,
	}
}

// noDialHistory is the SQL condition of a row that never recorded a dial.
const noDialHistory = "last_ok IS NULL AND last_fail IS NULL"

// successBase is the SQL value the next success rate decays from: successPrior
// for a row without dial history, else the stored rate.
var successBase = "(CASE WHEN " + noDialHistory + " THEN ? ELSE success_ewma END)"

// RecordDialSuccess records a successful dial of the endpoint of ref at at: it
// sets LastOK, ends the failure streak and moves the success rate and the
// latency towards this dial.
func (db *DB) RecordDialSuccess(ref healthRef, at time.Time, latency time.Duration) error {
	at = at.UTC()
	row := ref.newRow(at)
	row.LastOK = &at
	row.SuccessEWMA = ewmaAlpha + (1-ewmaAlpha)*successPrior
	row.LatencyEWMA = latency

	return db.Clauses(clause.OnConflict{
		Columns: healthKey,
		DoUpdates: clause.Assignments(map[string]any{
			"last_ok":      at,
			"fail_streak":  0,
			"success_ewma": gorm.Expr("? + ? * "+successBase, ewmaAlpha, 1-ewmaAlpha, successPrior),
			// why: the first success seeds the latency, so a slow first dial is not halved by a zero.
			"latency_ewma": gorm.Expr("CASE WHEN last_ok IS NULL THEN ? ELSE CAST(? * ? + ? * latency_ewma AS INTEGER) END",
				int64(latency), ewmaAlpha, int64(latency), 1-ewmaAlpha),
			"updated_at": at,
		}),
	}).Create(row).Error
}

// RecordDialFailure records a failed dial of the endpoint of ref at at: it
// sets LastFail, extends the failure streak and moves the success rate
// towards 0.
func (db *DB) RecordDialFailure(ref healthRef, at time.Time) error {
	at = at.UTC()
	row := ref.newRow(at)
	row.LastFail = &at
	row.FailStreak = 1
	row.SuccessEWMA = (1 - ewmaAlpha) * successPrior

	return db.Clauses(clause.OnConflict{
		Columns: healthKey,
		DoUpdates: clause.Assignments(map[string]any{
			"last_fail":    at,
			"fail_streak":  gorm.Expr("fail_streak + 1"),
			"success_ewma": gorm.Expr("? * "+successBase, 1-ewmaAlpha, successPrior),
			"updated_at":   at,
		}),
	}).Create(row).Error
}

// RecordEndpointSeen records that a first-hand reply listed the endpoint of
// nodeID at at, under Scope "": it sets FirstSeen once, sets LastSeen, and
// counts the UTC day of at unless LastSeen is already on it.
// note: it leaves the dial fields and never extends any expiry.
func (db *DB) RecordEndpointSeen(nodeID *astral.Identity, endpoint endpointKey, at time.Time) error {
	at = at.UTC()
	dayStart := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	row := healthRef{identity: nodeID, endpoint: endpoint}.newRow(at)
	row.FirstSeen, row.LastSeen, row.SeenDays = &at, &at, 1

	return db.Clauses(clause.OnConflict{
		Columns: healthKey,
		DoUpdates: clause.Assignments(map[string]any{
			"first_seen": gorm.Expr("COALESCE(first_seen, ?)", at),
			"seen_days": gorm.Expr("CASE WHEN last_seen IS NULL OR last_seen < ? THEN seen_days + 1 ELSE seen_days END",
				dayStart),
			"last_seen":  at,
			"updated_at": at,
		}),
	}).Create(row).Error
}

// FindEndpointHealth returns the health rows of nodeID under scopes.
func (db *DB) FindEndpointHealth(nodeID *astral.Identity, scopes ...string) (rows []*dbEndpointHealth, err error) {
	err = db.Where("identity = ? AND scope IN ?", nodeID, scopes).Find(&rows).Error
	return
}

// DeleteStaleEndpointHealth deletes the health rows last updated before before.
func (db *DB) DeleteStaleEndpointHealth(before time.Time) (int64, error) {
	tx := db.Where("updated_at < ?", before.UTC()).Delete(&dbEndpointHealth{})
	return tx.RowsAffected, tx.Error
}
