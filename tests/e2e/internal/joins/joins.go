// Package joins admits a node into the User's swarm the way user.adopt did
// before G7: the node joins through a carrier (user.join) as a Member. The
// carrier signs the membership with the node in the carrier session, so no
// answer of the User is needed (astral-docs protocols/user/README.md#joins).
package joins

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"
)

// Wait bounds an admission: the join and the carrier session.
const Wait = 60 * time.Second

// Admit has joiner, a client of the joining node's own token, join through
// carrier as a Member. carrierID is the carrier's identity, as a session view
// names it. It returns the membership the join handed over.
func Admit(ctx *astral.Context, joiner *astrald.Client, carrierID string) (*auth.SignedContract, error) {
	carrier, err := astral.ParseIdentity(carrierID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(Wait)
	defer cancel()
	defer contracts.Waiting()()

	membership, err := join(ctx, joiner, carrier)
	if err != nil {
		return nil, fmt.Errorf("user.join through %v: %w", carrier, err)
	}
	return membership, nil
}

// join runs user.join through carrier and returns the membership it answers.
func join(ctx *astral.Context, c *astrald.Client, carrier *astral.Identity) (*auth.SignedContract, error) {
	ch, err := c.QueryChannel(ctx, "user.join", query.Args{"node": carrier.String(), "tier": "member"})
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	var membership *auth.SignedContract
	err = ch.Switch(
		func(sc *auth.SignedContract) error { membership = sc; return channel.ErrBreak },
		channel.PassErrors, channel.WithContext(ctx), channel.FailOnEOF(),
	)
	if membership == nil && err == nil {
		err = errors.New("no membership")
	}
	return membership, err
}
