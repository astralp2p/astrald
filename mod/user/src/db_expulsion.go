package user

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user"
)

// dbExpulsion stores one ban per (issuer, subject) pair, decomposed into columns
// that rebuild the SignedExpulsion (mirroring how dbContract rebuilds a
// SignedContract — no whole object held). Rows are append-only: a ban is never
// updated or removed, matching the irreversible ban semantics.
type dbExpulsion struct {
	IssuerID   *astral.Identity `gorm:"primaryKey"`
	SubjectID  *astral.Identity `gorm:"primaryKey"`
	ExpelledAt time.Time
	IssuerSig  []byte
}

func (dbExpulsion) TableName() string { return user.DBPrefix + "expulsions" }
