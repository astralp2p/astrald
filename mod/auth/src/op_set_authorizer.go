package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAuthorizerArgs struct {
	// Action is the action object type, e.g. mod.auth.see_objects_action.
	Action string `query:"required"`
	// Endpoint is an http(s):// URL or astral://<identity-or-alias>:<op>.
	Endpoint string `query:"required"`
	// Actor is the identity or alias the assignment answers for. Empty makes it
	// action-wide.
	Actor string
	// Timeout bounds one question. Zero takes the default.
	Timeout astral.Duration
	// Token is the bearer token for an http endpoint.
	Token string
	In    string
	Out   string
}

// OpSetAuthorizer creates or replaces the external authorizer assignment for an
// action type and, optionally, one actor. It answers Ack once the assignment is
// persisted and applies to the next authorization.
//
// why AdminManageApps: an assignment decides what an app may do on this node,
// which is the administration of app authority that action covers.
func (mod *Module) OpSetAuthorizer(ctx *astral.Context, q *routing.IncomingQuery, args opSetAuthorizerArgs) error {
	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	a := &auth.AuthorizerAssignment{
		Action:     astral.String8(args.Action),
		Endpoint:   astral.String8(args.Endpoint),
		Timeout:    args.Timeout,
		TokenValue: astral.String8(args.Token),
	}

	if args.Actor != "" {
		actor, err := mod.Dir.ResolveIdentity(args.Actor)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
		a.Actor = actor
	}

	if err := mod.SetAuthorizer(ctx, a); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "authorizer for %v %v set to %v", args.Action, a.Actor, args.Endpoint)

	return ch.Send(&astral.Ack{})
}
