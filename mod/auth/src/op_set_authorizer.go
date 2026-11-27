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
// It admits only a local origin (astral.ErrOriginNotAllowed otherwise).
//
// why AdminManageApps: an assignment decides what an app may do on this node,
// which is the administration of app authority that action covers.
//
// why the origin: every AdminManageApps op that mints authority is local-only
// (apphost.create_token refuses a network origin), and a relayed query arrives
// with a network origin and the relayed caller.
//
// why no further check for an assignment for a user power: a local holder of
// AdminManageApps mints tokens that authenticate as any identity, the user
// included (apphost.create_token), so an authority it installs grants it
// nothing it lacks.
func (mod *Module) OpSetAuthorizer(ctx *astral.Context, q *routing.IncomingQuery, args opSetAuthorizerArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: the origin is admitted by name rather than refused by name, so an
	// origin added later is refused until it is judged.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

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
