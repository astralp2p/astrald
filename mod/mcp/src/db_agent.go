package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/mcp"
)

type dbAgent struct {
	Identity *astral.Identity `gorm:"uniqueIndex"`
	Alias    string
	// note: the issued token is stored so delete_agent can revoke it —
	// apphost deletes access tokens by token string, not identity.
	Token     string `gorm:"uniqueIndex"`
	ExpiresAt time.Time
	CreatedAt time.Time
	// why false by default: an agent nobody has opted in is unreachable, so a
	// row written before this column existed, or by code that forgets it,
	// fails closed. The inverse spelling would fail open.
	Exposed bool
}

func (dbAgent) TableName() string {
	return mcp.DBPrefix + "agents"
}
