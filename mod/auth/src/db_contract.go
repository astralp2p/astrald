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
}

func (dbContract) TableName() string { return auth.DBPrefix + "contracts" }
