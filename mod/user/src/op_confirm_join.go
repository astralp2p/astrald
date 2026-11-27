package user

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// Errors user.confirm_join sends besides those of user.answer_request. Their
// text is part of the protocol (astral-docs
// protocols/user/ops/user.confirm_join.md).
var (
	// errNoUserKey: this node cannot reach the user key.
	errNoUserKey = astral.NewError("no user key on this node")
	// errNotPrivilegedJoin is sent when the id names a stored request that is
	// not a privileged_member JoinRequest.
	errNotPrivilegedJoin = astral.NewError("not a privileged join request")
)

type opConfirmJoinArgs struct {
	ID  *astral.ObjectID `query:"required"`
	In  string
	Out string
}

// OpConfirmJoin is the user's confirmation of the privileged_member
// JoinRequest whose entry ID is ID: the user key signs the joiner's
// PrivilegedMembership as issuer and sends it. It adds nothing to SwarmState:
// the carrier of the join hands it to the joiner, who co-signs it in the
// carrier session, and the carrier stores it with both signatures. Only the
// user identity may call it, and only from a local origin on this node; the
// request is found in user.swarm_requests.
// Spec: .ai/system/protocols/user/ops/user.confirm_join.md
//
// It refuses, writing nothing:
//   - astral.ErrOriginNotAllowed when the query is not local;
//   - errNoUserKey when this node cannot reach the user key;
//   - auth.ErrNotPermitted when the caller is not the user;
//   - errUnknownRequest when ID names no stored request;
//   - errNotPrivilegedJoin when the request is not a privileged join;
//   - errRequestClosed when the request is declined or expired, or its
//     joiner is expelled.
//
// A second confirmation sends the membership the first one signed, or the
// one stored once the joiner co-signed it.
//
// why: no named action guards this op; the caller must be the user identity
// itself, which no grant stands in for (confirmJoin).
func (mod *Module) OpConfirmJoin(ctx *astral.Context, q *routing.IncomingQuery, args opConfirmJoinArgs) (err error) {
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

	signed, err := mod.confirmJoin(ctx, q.Caller(), args.ID)
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
func (mod *Module) confirmJoin(ctx *astral.Context, caller *astral.Identity, id *astral.ObjectID) (*auth.SignedContract, error) {
	uk := mod.userKeyCeremony()
	if uk == nil {
		return nil, errNoUserKey
	}
	if !caller.IsEqual(uk.user) {
		return nil, auth.ErrNotPermitted
	}

	obj, err := mod.SwarmEntry(id)
	if err != nil {
		return nil, errUnknownRequest
	}
	if kind, ok := user.EntryKindOf(obj); !ok || kind != user.EntryRequests {
		return nil, errUnknownRequest
	}
	req, ok := obj.(*user.SignedJoinRequest)
	if !ok || req.IsNil() || req.Tier != user.TierPrivilegedMember {
		return nil, errNotPrivilegedJoin
	}

	if sc := mod.carrier.confirmed.get(id); sc != nil {
		return sc, nil
	}
	if sc := storedPrivileged(mod, uk.user, id, req.Joiner); sc != nil {
		return sc, nil
	}

	switch mod.RequestStatus(id) {
	case user.RequestDeclined, user.RequestExpired:
		return nil, errRequestClosed
	}
	if req.Expired(uk.now()) || mod.Expelled(req.Joiner) {
		return nil, errRequestClosed
	}

	sc, err := uk.Membership(ctx, req.Joiner, user.TierPrivilegedMember, req)
	if err != nil {
		return nil, err
	}
	return mod.carrier.confirmed.keep(id, sc), nil
}

// storedPrivileged returns the PrivilegedMembership the user key userID issued
// to joiner for the JoinRequest reqID and the joiner co-signed, or nil.
func storedPrivileged(view usermod.SwarmView, userID *astral.Identity, reqID *astral.ObjectID, joiner *astral.Identity) *auth.SignedContract {
	for _, sc := range view.ContractsOf(reqID) {
		if !sc.IsNil() && sc.SubjectSig != nil && sc.Issuer.IsEqual(userID) && sc.Subject.IsEqual(joiner) &&
			user.TierOnly(sc.Contract, user.TierPrivilegedMember) {
			return sc
		}
	}
	return nil
}
