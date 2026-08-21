package auth

import (
	"bytes"
	"fmt"
	authmod "github.com/astralp2p/astrald/mod/auth"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

type dbContractPermit struct {
	ID       uint             `gorm:"primaryKey;autoIncrement"`
	ObjectID *astral.ObjectID `gorm:"index"`
	Name     string           `gorm:"index"`
	Data     []byte
}

func (dbContractPermit) TableName() string { return authmod.DBPrefix + "contract_permits" }

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
