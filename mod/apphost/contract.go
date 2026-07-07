package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

// NewAppContract creates an app contract granting RelayForAction from app to node.
func NewAppContract(app, node *astral.Identity, duration time.Duration) (*auth.Contract, error) {
	permits := []*auth.Permit{
		{Action: astral.String8(nodes.RelayForAction{}.ObjectType())},
	}

	return &auth.Contract{
		Issuer:    app,
		Subject:   node,
		Permits:   permits,
		ExpiresAt: astral.Time(time.Now().Add(duration)),
	}, nil
}
