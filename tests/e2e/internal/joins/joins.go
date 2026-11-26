// Package joins admits a node into the User's swarm the way user.adopt did
// before G7: the node joins through a carrier (user.join), and the User
// approves its JoinRequest on the carrier (user.answer_request).
package joins

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"
)

// Wait bounds an admission: the join, the answer and the carrier's signature.
const Wait = 60 * time.Second

// Admit has joiner, a client of the joining node's own token, join through
// carrier as a Member, while asUser, a client of the User on the carrier,
// approves its JoinRequest. carrier and joinerID are the nodes' identities,
// as a session view names them. It returns the membership the join handed
// over.
func Admit(ctx *astral.Context, joiner, asUser *astrald.Client, carrierID, joinerID string) (*auth.SignedContract, error) {
	carrier, err := astral.ParseIdentity(carrierID)
	if err != nil {
		return nil, err
	}
	joinerIdentity, err := astral.ParseIdentity(joinerID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(Wait)
	defer cancel()
	defer contracts.Waiting()()

	// why the join decides: the harness answerer (tests/lib/answerer.py) may
	// approve the request before approve finds it pending, so the approver
	// stops once the join answered.
	actx, stop := ctx.WithCancel()
	defer stop()
	approved := make(chan error, 1)
	go func() { approved <- approve(actx, asUser, joinerIdentity) }()

	membership, err := join(ctx, joiner, carrier)
	if err != nil {
		stop()
		if aerr := <-approved; aerr != nil && actx.Err() == nil {
			err = errors.Join(err, aerr)
		}
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

// approve answers the pending JoinRequest of joinerID with an Approval, once
// it reaches the carrier.
func approve(ctx *astral.Context, c *astrald.Client, joinerID *astral.Identity) error {
	for {
		id, err := pendingJoin(ctx, c, joinerID)
		if err != nil {
			return err
		}
		if id != nil {
			return answer(ctx, c, id)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no JoinRequest of %v reached the carrier: %w", joinerID, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// pendingJoin returns the entry ID of joinerID's pending JoinRequest, or nil.
func pendingJoin(ctx *astral.Context, c *astrald.Client, joinerID *astral.Identity) (*astral.ObjectID, error) {
	ch, err := c.QueryChannel(ctx, "user.swarm_requests", nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	// why triples: the op sends a request's entry ID, its status and the
	// signed request, in that order (astral-docs user.swarm_requests.md).
	var objs []astral.Object
	if err = ch.Switch(channel.Collect(&objs), channel.BreakOnEOS, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return nil, fmt.Errorf("user.swarm_requests: %w", err)
	}
	for i := 0; i+2 < len(objs); i += 3 {
		id, _ := objs[i].(*astral.ObjectID)
		req, ok := objs[i+2].(*user.SignedJoinRequest)
		if id != nil && ok && req.Joiner.IsEqual(joinerID) {
			return id, nil
		}
	}
	return nil, nil
}

// answer approves the request id. A request already approved counts: the
// harness answerer (tests/lib/answerer.py) may approve it first.
func answer(ctx *astral.Context, c *astrald.Client, id *astral.ObjectID) error {
	ch, err := c.QueryChannel(ctx, "user.answer_request", query.Args{"id": id.String(), "answer": "approve"})
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.Switch(func(*user.SignedApproval) error { return channel.ErrBreak },
		channel.PassErrors, channel.FailOnEOF())
	if err != nil && strings.Contains(err.Error(), "request is not open to this answer") {
		return nil
	}
	return err
}
