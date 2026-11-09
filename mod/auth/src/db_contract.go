package auth

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/auth"
)

type dbContract struct {
	ObjectID   *astral.ObjectID `gorm:"primaryKey"`
	IssuerID   *astral.Identity `gorm:"index"`
	SubjectID  *astral.Identity `gorm:"index"`
	IssuerSig  []byte
	SubjectSig []byte
	StartsAt   time.Time
	ExpiresAt  time.Time
	// WithdrawnAt is set when the operator took the contract back on this node.
	// why a mark and not a delete: the indexer and a sibling's push index a
	// contract again, and the row is what makes IndexContract skip it.
	WithdrawnAt *time.Time
}

func (dbContract) TableName() string { return auth.DBPrefix + "contracts" }
