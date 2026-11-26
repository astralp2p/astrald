package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
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
// It admits only a local origin (astral.ErrOriginNotAllowed otherwise), and an
// assignment for a SudoAction, an ExpelAction or a RelayForAction only from the
// user (auth.ErrNotPermitted otherwise).
//
// why AdminManageApps: an assignment decides what an app may do on this node,
// which is the administration of app authority that action covers.
//
// why the origin: the swarm root answers AdminManageApps for a Privileged node
// at any hop, so a networked query would let another node install an authority
// on this one.
//
// why the user for those three: the swarm root never answers a sudo or an
// expel, and relays only as the user (D2, D6, D7). An authority for one of them
// would answer what the root does not, so only the user installs one.
func (mod *Module) OpSetAuthorizer(ctx *astral.Context, q *routing.IncomingQuery, args opSetAuthorizerArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if userOnlyAuthorizer(args.Action) && !mod.isUser(q.Caller()) {
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

// userOnlyAuthorizer reports whether an authority for action answers what the
// swarm root never does, so that only the user may install one.
func userOnlyAuthorizer(action string) bool {
	switch action {
	case auth.SudoAction{}.ObjectType(), user.ExpelAction{}.ObjectType(), nodes.RelayForAction{}.ObjectType():
		return true
	}
	return false
}

// isUser reports whether id is this node's user. Without the user module, or
// on an unclaimed node, nobody is.
func (mod *Module) isUser(id *astral.Identity) bool {
	if mod.User == nil {
		return false
	}
	userID := mod.User.Identity()
	return !userID.IsZero() && id.IsEqual(userID)
}
