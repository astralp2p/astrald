package apphost

import (
	"bytes"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// dbGrant is a permit this node holds for an identity, recorded rather than
// signed. It authorizes on this node only and is never handed to anyone.
//
// Rows accumulate: an identity can hold several grants for one action, and
// authorization allows when any unexpired row allows. Identity, Name and Data
// are unique together. Data is the encoded permit with Delegation 0, so equal
// bytes are an identical permit, and granting it again refreshes ExpiresAt.
// Narrowing is granting the narrow permit, then revoking the wide row by ID.
//
// ExpiresAt is nil for a grant that does not expire.
//
// why AUTOINCREMENT on ID: a revoke names one row by its ID, and without it the
// pure-Go sqlite driver reuses a deleted max ID, so a stale listing could
// revoke a newer grant.
type dbGrant struct {
	ID        uint             `gorm:"primaryKey;autoIncrement;type:integer PRIMARY KEY AUTOINCREMENT"`
	Identity  *astral.Identity `gorm:"uniqueIndex:idx_grants_identity_name_data"`
	Name      string           `gorm:"uniqueIndex:idx_grants_identity_name_data"`
	Data      []byte           `gorm:"uniqueIndex:idx_grants_identity_name_data"`
	ExpiresAt *time.Time       `gorm:"index"`
	CreatedAt time.Time
}

// legacyGrantIndex is the unique (identity, name) index of the replacing
// grants table.
const legacyGrantIndex = "idx_grants_identity_name"

// grantsExpiryIndex is the index gorm names for dbGrant.ExpiresAt.
const grantsExpiryIndex = "idx_apphost__grants_expires_at"

func (dbGrant) TableName() string { return apphostmod.DBPrefix + "grants" }

func toGrantPermit(row *dbGrant) (*auth.Permit, error) {
	return astral.DecodeAs[*auth.Permit](row.Data)
}

func fromGrantPermit(identity *astral.Identity, p *auth.Permit, expiresAt *time.Time) (*dbGrant, error) {
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, p); err != nil {
		return nil, fmt.Errorf("encode permit: %w", err)
	}

	// why: FindGrants compares the expiry against a UTC clock and the pure-Go
	// sqlite driver compares datetimes as text, so the stored side is UTC too
	// whatever zone the caller built it in. The copy leaves the caller's value
	// as it was.
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
	}

	return &dbGrant{
		Identity:  identity,
		Name:      string(p.Action),
		Data:      buf.Bytes(),
		ExpiresAt: expiresAt,
	}, nil
}
