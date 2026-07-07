package user

import (
	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/user"
)

type opAdoptArgs struct {
	Target string
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

// OpAdopt adopts a target node into the active contract and indexes the signed result.
// Requires an active contract; the caller must be authorized for user.AdoptAction
// (code 4 otherwise) - the user always is, other identities via authorizers.
// Pushes the signed contract to the local swarm asynchronously after indexing.
func (mod *Module) OpAdopt(ctx *astral.Context, q *routing.IncomingQuery, args opAdoptArgs) (err error) {
	if mod.ActiveContract() == nil {
		return q.RejectWithCode(2)
	}

	// resolve before authorization - the adopt action carries the target
	nodeID, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return q.RejectWithCode(3)
	}

	adopt := &user.AdoptAction{Action: auth.NewAction(q.Caller()), Subject: nodeID}
	if !mod.Auth.Authorize(ctx, adopt) {
		return q.RejectWithCode(4)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// issue a membership contract for the node
	signed, err := mod.IssueMembership(ctx, nodeID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.Auth.IndexContract(ctx, signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	go mod.PushToLocalSwarm(mod.ctx, signed)

	// why: PushToLocalSwarm only sends the new contract, leaving the invitee
	// without the inviter's own and sibling contracts. The LinkCreatedEvent
	// trigger already fired before indexing, so sync the joined node here.
	mod.Scheduler.Schedule(mod.NewSyncNodesTask(signed.Subject))

	mod.log.Info("signed contract with %v", nodeID)
	return ch.Send(signed)
}
