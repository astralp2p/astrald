package crypto

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/crypto"
)

type dbPrivateKey struct {
	KeyID       *astral.ObjectID `gorm:"uniqueIndex"` // ID of the PrivateKey
	Type        string           `gorm:"index"`       // key type
	PublicKeyID *astral.ObjectID `gorm:"uniqueIndex"` // ID of the corresponding PublicKey
	PublicKey   string           `gorm:"uniqueIndex"` // public key in text format for lookups
}

func (dbPrivateKey) TableName() string {
	return crypto.DBPrefix + "private_keys"
}
