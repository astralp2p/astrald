package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveAuthorizerArgs struct {
	// Action is the action object type of the assignment.
	Action string `query:"required"`
	// Actor is the identity or alias of an actor-specific assignment. Empty
	// removes the action-wide one.
	Actor string
	In    string
	Out   string
}

// OpRemoveAuthorizer removes the external authorizer assignment for an action
// type and, optionally, one actor. It answers Ack once the removal is persisted.
// Removing an actor's assignment makes the action-wide one apply to it again.
func (mod *Module) OpRemoveAuthorizer(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveAuthorizerArgs) error {
	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var actor *astral.Identity
	if args.Actor != "" {
		var err error
		if actor, err = mod.Dir.ResolveIdentity(args.Actor); err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	if err := mod.RemoveAuthorizer(ctx, args.Action, actor); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "authorizer for %v %v removed", args.Action, actor)

	return ch.Send(&astral.Ack{})
}
