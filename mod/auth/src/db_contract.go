package auth

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
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
