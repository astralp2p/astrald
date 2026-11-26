package user

import (
	"errors"
	usermod "github.com/astralp2p/astrald/mod/user"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// Errors user.confirm_join sends besides those of user.answer_request. Their
// text is part of the protocol (astral-docs
// protocols/user/ops/user.confirm_join.md).
var (
	// ErrNoUserKey: this node cannot reach the user key.
	ErrNoUserKey = astral.NewError("no user key on this node")
	// ErrNotPrivilegedJoin: the id names a stored request that is not a
	// privileged_member JoinRequest.
	ErrNotPrivilegedJoin = astral.NewError("not a privileged join request")
)

type opConfirmJoinArgs struct {
	ID  *astral.ObjectID `query:"required"`
	In  string
	Out string
}

// OpConfirmJoin is the user's confirmation of the privileged_member
// JoinRequest whose entry ID is ID: the user key signs the joiner's
// PrivilegedMembership, adds it to SwarmState and sends it. The carrier of the
// join hands it to the joiner. Only the user identity may call it, and only
// from a local origin on this node; the request is found in
// user.swarm_requests.
//
// It refuses, writing nothing:
//   - astral.ErrOriginNotAllowed when the query is not local;
//   - ErrNoUserKey when this node cannot reach the user key;
//   - auth.ErrNotPermitted when the caller is not the user;
//   - ErrUnknownRequest when ID names no stored request;
//   - ErrNotPrivilegedJoin when the request is not a privileged join;
//   - ErrRequestClosed when the request is declined or expired, or its
//     joiner is expelled.
//
// A second confirmation sends the membership the first one stored.
func (ops *SwarmAnswerOps) OpConfirmJoin(ctx *astral.Context, q *routing.IncomingQuery, args opConfirmJoinArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why local only: a member allowed to relay as the user reaches this node
	// with the user as caller, so over the network the caller is no proof of
	// the user's own action (D2).
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	signed, err := ops.confirmJoin(ctx, q.Caller(), args.ID)
	if err != nil {
		var aerr astral.Error
		if errors.As(err, &aerr) {
			return ch.Send(aerr)
		}
		return ch.Send(astral.Err(err))
	}
	return ch.Send(signed)
}

// confirmJoin checks the caller and the request, and has the user key sign
// the joiner's PrivilegedMembership.
//
// why caller and not Auth: the confirmation is the user's own action that a
// software user key cannot take for itself, so no app or grant stands in for
// the user identity. OpConfirmJoin admits only local origins, so a node
// relaying as the user cannot either.
func (ops *SwarmAnswerOps) confirmJoin(ctx *astral.Context, caller *astral.Identity, id *astral.ObjectID) (*auth.SignedContract, error) {
	var uk *userKeyCeremony
	if ops.UserKey != nil {
		uk = ops.UserKey()
	}
	if uk == nil {
		return nil, ErrNoUserKey
	}
	if !caller.IsEqual(uk.user) {
		return nil, auth.ErrNotPermitted
	}

	obj, err := ops.State.SwarmEntry(id)
	if err != nil {
		return nil, ErrUnknownRequest
	}
	if kind, ok := user.EntryKindOf(obj); !ok || kind != user.EntryRequests {
		return nil, ErrUnknownRequest
	}
	req, ok := obj.(*user.SignedJoinRequest)
	if !ok || req.IsNil() || req.Tier != user.TierPrivilegedMember {
		return nil, ErrNotPrivilegedJoin
	}

	if sc := userKeyMembership(ops.View, uk.user, req.Joiner); sc != nil {
		return sc, nil
	}

	switch ops.View.RequestStatus(id) {
	case user.RequestPending, user.RequestApproved:
	default:
		return nil, ErrRequestClosed
	}
	if req.Expired(uk.now()) || ops.View.Expelled(req.Joiner) {
		return nil, ErrRequestClosed
	}

	sc, err := uk.Membership(ctx, req.Joiner, user.TierPrivilegedMember, id)
	if err != nil {
		return nil, err
	}
	if err = ops.State.AddSwarmEntry(ctx, sc); err != nil && !errors.Is(err, usermod.ErrSwarmEntryHeld) {
		return nil, err
	}
	return sc, nil
}
