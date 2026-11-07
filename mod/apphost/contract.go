package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// NewAppContract creates an app contract granting RelayForAction from app to node.
//
// why: the relay contract is part of registration, and no policy withholds it;
// a swarm sibling reaches an app only through it (decided 2026-11-06).
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
