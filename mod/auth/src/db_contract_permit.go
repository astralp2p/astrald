package auth

import (
	"bytes"
	"fmt"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

type dbContractPermit struct {
	ID       uint             `gorm:"primaryKey;autoIncrement"`
	ObjectID *astral.ObjectID `gorm:"index"`
	Name     string           `gorm:"index"`
	Data     []byte
}

func (dbContractPermit) TableName() string { return auth.DBPrefix + "contract_permits" }

func toPermit(row *dbContractPermit) (*auth.Permit, error) {
	return astral.DecodeAs[*auth.Permit](row.Data)
}

func fromPermit(objectID *astral.ObjectID, p *auth.Permit) (*dbContractPermit, error) {
	var buf bytes.Buffer
	_, err := astral.Encode(&buf, p)
	if err != nil {
		return nil, fmt.Errorf("encode permit: %w", err)
	}
	return &dbContractPermit{ObjectID: objectID, Name: string(p.Action), Data: buf.Bytes()}, nil
}
