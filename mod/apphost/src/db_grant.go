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
// Identity and Name are unique together, so granting is an upsert: widening,
// narrowing or replacing what an identity may do is one row, one statement, and
// no signature. That is the property a signed contract cannot offer, and the
// reason grants exist beside them.
//
// ExpiresAt is nil for a grant that does not expire.
type dbGrant struct {
	ID        uint             `gorm:"primaryKey;autoIncrement"`
	Identity  *astral.Identity `gorm:"uniqueIndex:idx_grants_identity_name"`
	Name      string           `gorm:"uniqueIndex:idx_grants_identity_name"`
	Data      []byte
	ExpiresAt *time.Time `gorm:"index"`
	CreatedAt time.Time
}

func (dbGrant) TableName() string { return apphostmod.DBPrefix + "grants" }

func toGrantPermit(row *dbGrant) (*auth.Permit, error) {
	return astral.DecodeAs[*auth.Permit](row.Data)
}

func fromGrantPermit(identity *astral.Identity, p *auth.Permit, expiresAt *time.Time) (*dbGrant, error) {
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, p); err != nil {
		return nil, fmt.Errorf("encode permit: %w", err)
	}

	return &dbGrant{
		Identity:  identity,
		Name:      string(p.Action),
		Data:      buf.Bytes(),
		ExpiresAt: expiresAt,
	}, nil
}
