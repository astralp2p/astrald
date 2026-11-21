package nodes

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// legacyHealth is the health row as the schema before the dial scores stored it.
type legacyHealth struct {
	Identity    *astral.Identity `gorm:"primaryKey"`
	Network     string           `gorm:"primaryKey"`
	Address     string           `gorm:"primaryKey"`
	Scope       string           `gorm:"primaryKey"`
	LastOK      *time.Time       `gorm:"column:last_ok;index"`
	LastFail    *time.Time       `gorm:"column:last_fail"`
	OKCount     uint32           `gorm:"column:ok_count"`
	FailStreak  uint32           `gorm:"column:fail_streak"`
	LastLatency time.Duration    `gorm:"column:last_latency"`
	UpdatedAt   time.Time        `gorm:"column:updated_at;index"`
}

func (legacyHealth) TableName() string { return dbEndpointHealth{}.TableName() }

// legacyDB returns a database whose health table holds one row per address in
// the legacy schema, each with a last success, before the migration to the current one.
func legacyDB(t *testing.T, id *astral.Identity, addrs ...string) *DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gdb.AutoMigrate(&legacyHealth{}); err != nil {
		t.Fatalf("migrate legacy: %v", err)
	}
	at := time.Now().UTC().Add(-time.Hour)
	for _, a := range addrs {
		row := &legacyHealth{Identity: id, Network: "tcp", Address: a, LastOK: &at, OKCount: 5, UpdatedAt: at}
		if err := gdb.Create(row).Error; err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
	}
	db := &DB{gdb}
	if err := db.AutoMigrate(&dbEndpointHealth{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestRecordDialRecoversLegacyRow: a row stored before success_ewma and
// latency_ewma existed has both NULL; the upserts still move them, so the row
// ranks by its dials. It runs without the backfill, so the upserts alone are held to it.
func TestRecordDialRecoversLegacyRow(t *testing.T) {
	id := astral.GenerateIdentity()
	db := legacyDB(t, id, "198.51.100.1:1791")
	r := ref(id, "198.51.100.1:1791")

	at := time.Now().UTC()
	for range 3 {
		if err := db.RecordDialSuccess(r, at, 200*time.Millisecond); err != nil {
			t.Fatalf("RecordDialSuccess: %v", err)
		}
	}
	row := healthOf(t, db, id, "198.51.100.1:1791")
	if want := ewmaOf(successPrior, 1, 1, 1); !approx(row.SuccessEWMA, want) {
		t.Fatalf("SuccessEWMA after 3 successes = %v; want %v", row.SuccessEWMA, want)
	}
	if row.LatencyEWMA != 200*time.Millisecond {
		t.Fatalf("LatencyEWMA = %v; want 200ms", row.LatencyEWMA)
	}
}

// TestRecordDialFailureRecoversLegacyRow: a failure moves the NULL success
// rate of a legacy row towards 0 from successPrior instead of leaving it NULL.
func TestRecordDialFailureRecoversLegacyRow(t *testing.T) {
	id := astral.GenerateIdentity()
	db := legacyDB(t, id, "198.51.100.2:1791")

	if err := db.RecordDialFailure(ref(id, "198.51.100.2:1791"), time.Now().UTC()); err != nil {
		t.Fatalf("RecordDialFailure: %v", err)
	}
	row := healthOf(t, db, id, "198.51.100.2:1791")
	if want := ewmaOf(successPrior, 0); !approx(row.SuccessEWMA, want) {
		t.Fatalf("SuccessEWMA after a failure = %v; want %v", row.SuccessEWMA, want)
	}
}

// TestBackfillDialColumns: the backfill gives a legacy row the neutral success
// rate and no latency, so it ranks as an endpoint that works half the time
// before its next dial, and it leaves a row that has real values alone.
func TestBackfillDialColumns(t *testing.T) {
	id := astral.GenerateIdentity()
	db := legacyDB(t, id, "198.51.100.3:1791")
	if err := db.RecordDialSuccess(ref(id, "198.51.100.4:1791"), time.Now().UTC(), time.Second); err != nil {
		t.Fatalf("RecordDialSuccess: %v", err)
	}
	want := healthOf(t, db, id, "198.51.100.4:1791").SuccessEWMA

	for range 2 { // twice: it is idempotent
		if err := db.backfillDialColumns(); err != nil {
			t.Fatalf("backfill: %v", err)
		}
	}
	legacy := healthOf(t, db, id, "198.51.100.3:1791")
	if !approx(legacy.SuccessEWMA, successPrior) || legacy.LatencyEWMA != 0 {
		t.Fatalf("legacy row = %v, %v; want %v, 0", legacy.SuccessEWMA, legacy.LatencyEWMA, successPrior)
	}
	if got := healthOf(t, db, id, "198.51.100.4:1791").SuccessEWMA; !approx(got, want) {
		t.Fatalf("recorded row SuccessEWMA = %v after backfill; want %v", got, want)
	}
}
