package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAdoptArgs struct {
	Identity string `query:"required"`
	In       string
	Out      string
}

// OpAdopt adopts the named node into the active contract and indexes the signed result.
// Requires an active contract; the caller must be authorized for
// user.AdminSwarmAction (code 4 otherwise) - the user always is, other
// identities via authorizers.
// Pushes the signed contract to the local swarm asynchronously after indexing.
func (mod *Module) OpAdopt(ctx *astral.Context, q *routing.IncomingQuery, args opAdoptArgs) (err error) {
	if mod.ActiveContract() == nil {
		return q.RejectWithCode(2)
	}

	// why: the action carries the node, so resolution runs before authorization.
	// The empty name and "anyone" resolve to the zero identity, which names no
	// node, so the name is refused here instead of adopted.
	nodeID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil || nodeID.IsZero() {
		return q.RejectWithCode(3)
	}

	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(q.Caller()),
		Subject: nodeID,
	}) {
		return q.RejectWithCode(4)
	}

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: issuing the membership waits on the adopted node's invite policy,
	// which may be delegated there and wait on its user; only the conn tells
	// that wait that nobody waits for the adoption any more.
	issueCtx, stop := watchRequester(ctx, conn)
	defer stop()

	// issue a membership contract for the node
	signed, err := mod.IssueMembership(issueCtx, nodeID)
	if err != nil {
		if issueCtx.Err() != nil {
			mod.log.Logv(1, "adoption of %v: requester left before the node signed", nodeID)
			return nil
		}
		return ch.Send(astral.Err(err))
	}

	// why a countersigned contract is recorded even when the requester left:
	// the adopted node has already installed it, so recording it here is the
	// only outcome that keeps the swarm consistent.

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
