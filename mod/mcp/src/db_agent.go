package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

type dbAgent struct {
	Identity *astral.Identity `gorm:"uniqueIndex"`
	Alias    string
	// note: the issued token is stored so delete_agent can revoke it —
	// apphost deletes access tokens by token string, not identity.
	Token     string `gorm:"uniqueIndex"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (dbAgent) TableName() string {
	return mcpmod.DBPrefix + "agents"
}
