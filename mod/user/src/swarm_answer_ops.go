package user

import (
	"errors"
	"sort"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/crypto"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// Errors the answering ops send. Their text is part of the protocol
// (astral-docs protocols/user/ops/user.swarm_requests.md and
// user.answer_request.md).
var (
	// ErrUnknownRequest: the id names no stored request.
	ErrUnknownRequest = astral.NewError("unknown request")
	// ErrOutsideTier: this node has no membership, or its tier may not answer
	// that request (user.MayAnswer).
	ErrOutsideTier = astral.NewError("outside this node's tier")
	// ErrRequestClosed: the request's status admits no such answer.
	ErrRequestClosed = astral.NewError("request is not open to this answer")
	// ErrInvalidAnswer: the answer argument is neither approve nor decline.
	ErrInvalidAnswer = astral.NewError("invalid answer")
)

// Words of the answer argument of user.answer_request.
const (
	answerApprove = "approve"
	answerDecline = "decline"
)

// swarmAuthorizer is the part of the auth module the answering ops use.
type swarmAuthorizer interface {
	Authorize(ctx *astral.Context, action auth.ActionObject) bool
}

// SwarmAnswerOps serves user.swarm_requests and user.answer_request: it lists
// the requests in SwarmState with their status, and answers one with an
// Approval or a Decline signed by this node, only within this node's tier.
//
// It also serves user.confirm_join, through which the user has its key sign
// a pending Privileged JoinRequest (swarm_confirm_op.go).
//
// It reaches SwarmState only through nodes.SwarmState and reads what entries
// mean only through user.SwarmView. Nothing registers it yet: G7 builds it
// with mod.Nodes, the SwarmView implementation, mod.Auth,
// mod.Crypto.NodeSigner() and, on a node that reaches the user key, the
// userKeyCeremony its carrier uses, and adds it to the module's router with
// AddStructPrefix(ops, "Op").
type SwarmAnswerOps struct {
	Node   *astral.Identity
	State  nodesmod.SwarmState
	View   usermod.SwarmView
	Auth   swarmAuthorizer
	Signer crypto.HashSigner
	// UserKey returns the ceremony that signs for user.confirm_join; nil:
	// this node cannot reach the user key, and user.confirm_join refuses.
	UserKey func() *userKeyCeremony
	// Root returns the root key, which answers every request with no
	// membership of its own (an unclaimed node is its own root).
	Root func() *astral.Identity
}

type opSwarmRequestsArgs struct {
	All bool
	Out string
}

// OpSwarmRequests authorizes the caller under SeeSwarm (Members may see
// requests), then streams the requests stored in SwarmState, oldest filed
// first. Without All it lists only pending requests. For each request it sends
// three objects: the request's entry ID (object_id), its status
// (mod.user.request_status), and the stored signed request. It ends with EOS.
func (ops *SwarmAnswerOps) OpSwarmRequests(ctx *astral.Context, q *routing.IncomingQuery, args opSwarmRequestsArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !ops.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	list, err := ops.listRequests(args.All)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, l := range list {
		status := l.status
		for _, obj := range []astral.Object{l.id, &status, l.req} {
			if err = ch.Send(obj); err != nil {
				return err
			}
		}
	}

	return ch.Send(&astral.EOS{})
}

// listedRequest is one request user.swarm_requests sends.
type listedRequest struct {
	id      *astral.ObjectID
	status  user.RequestStatus
	req     astral.Object
	filedAt time.Time
}

// listRequests returns the stored requests, only the pending ones unless all
// is set, oldest filed first; requests filed at the same time sort by ID.
func (ops *SwarmAnswerOps) listRequests(all bool) ([]listedRequest, error) {
	ids, err := ops.State.SwarmEntries(user.EntryRequests)
	if err != nil {
		return nil, err
	}

	var list []listedRequest
	for _, id := range ids {
		status := ops.View.RequestStatus(id)
		if !all && status != user.RequestPending {
			continue
		}
		obj, err := ops.State.SwarmEntry(id)
		if err != nil {
			// why: an entry listed a moment ago cannot leave a grow-only
			// set, so a failed read is an I/O error; the rest still list.
			continue
		}
		filedAt, ok := requestFiledAt(obj)
		if !ok {
			continue
		}
		list = append(list, listedRequest{id: id, status: status, req: obj, filedAt: filedAt})
	}

	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].filedAt.Equal(list[j].filedAt) {
			return list[i].filedAt.Before(list[j].filedAt)
		}
		return list[i].id.String() < list[j].id.String()
	})
	return list, nil
}

type opAnswerRequestArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Answer string           `query:"required"`
	In     string
	Out    string
}

// OpAnswerRequest answers the request whose entry ID is ID with an Approval
// (Answer "approve") or a Decline (Answer "decline") signed by this node, adds
// it to SwarmState and sends the signed answer. The caller needs
// AdminSwarmAction, whose Subject is the identity the request names.
//
// It refuses, writing nothing:
//   - ErrUnknownRequest when ID names no stored request;
//   - ErrOutsideTier when this node has no membership or its tier may not
//     answer the request (user.MayAnswer);
//   - ErrRequestClosed when the status admits no such answer: an Approval
//     only of a pending request; a Decline of a pending or an approved one,
//     since a Decline beats an Approval even after it is carried out.
//     Nobody answers an expired or a declined request.
func (ops *SwarmAnswerOps) OpAnswerRequest(ctx *astral.Context, q *routing.IncomingQuery, args opAnswerRequestArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	signed, err := ops.answer(ctx, q.Caller(), args.ID, args.Answer)
	if err != nil {
		var aerr astral.Error
		if errors.As(err, &aerr) {
			return ch.Send(aerr)
		}
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}

// answer checks, signs and writes one answer. It returns the SignedApproval
// or SignedDecline it added to SwarmState.
func (ops *SwarmAnswerOps) answer(ctx *astral.Context, caller *astral.Identity, id *astral.ObjectID, answer string) (astral.Object, error) {
	if answer != answerApprove && answer != answerDecline {
		return nil, ErrInvalidAnswer
	}

	req, err := ops.State.SwarmEntry(id)
	if err != nil {
		return nil, ErrUnknownRequest
	}
	if kind, ok := user.EntryKindOf(req); !ok || kind != user.EntryRequests {
		return nil, ErrUnknownRequest
	}

	if !ops.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(caller),
		Subject: requestSubject(req),
	}) {
		return nil, auth.ErrNotPermitted
	}

	if !ops.mayAnswer(req) {
		return nil, ErrOutsideTier
	}

	switch ops.View.RequestStatus(id) {
	case user.RequestPending:
	case user.RequestApproved:
		if answer != answerDecline {
			return nil, ErrRequestClosed
		}
	default:
		return nil, ErrRequestClosed
	}

	signed, err := ops.sign(ctx, id, answer)
	if err != nil {
		return nil, err
	}
	// A held answer is signed and may still be stored after a later merge
	// (nodes.ErrSwarmEntryHeld), so it is sent like a stored one.
	if err = ops.State.AddSwarmEntry(ctx, signed); err != nil && !errors.Is(err, nodesmod.ErrSwarmEntryHeld) {
		return nil, err
	}
	return signed, nil
}

// mayAnswer reports whether this node may answer req: as the root key, or by
// the tier of its membership (user.MayAnswer).
func (ops *SwarmAnswerOps) mayAnswer(req astral.Object) bool {
	if ops.Root != nil && isRoot(ops.Root(), ops.Node) {
		return true
	}
	membership, ok := ops.View.Membership(ops.Node)
	return ok && user.MayAnswer(membership.Tier, req)
}

// sign signs this node's Approval or Decline of the request id.
func (ops *SwarmAnswerOps) sign(ctx *astral.Context, id *astral.ObjectID, answer string) (astral.Object, error) {
	if answer == answerApprove {
		body := user.NewApproval(ops.Node, id)
		sig, err := ops.Signer.SignHash(ctx, body.SignableHash())
		if err != nil {
			return nil, err
		}
		return &user.SignedApproval{Approval: body, IssuerSig: sig}, nil
	}
	body := user.NewDecline(ops.Node, id)
	sig, err := ops.Signer.SignHash(ctx, body.SignableHash())
	if err != nil {
		return nil, err
	}
	return &user.SignedDecline{Decline: body, IssuerSig: sig}, nil
}

// requestFiledAt returns when a stored signed request was filed.
func requestFiledAt(obj astral.Object) (time.Time, bool) {
	switch r := obj.(type) {
	case *user.SignedContractRequest:
		if !r.IsNil() {
			return r.FiledAt.Time(), true
		}
	case *user.SignedJoinRequest:
		if !r.IsNil() {
			return r.FiledAt.Time(), true
		}
	case *user.SignedExpelRequest:
		if !r.IsNil() {
			return r.FiledAt.Time(), true
		}
	}
	return time.Time{}, false
}

// requestSubject returns the identity a request names: the ContractRequest's
// Identity, the JoinRequest's Joiner, or the ExpelRequest's Subject.
func requestSubject(obj astral.Object) *astral.Identity {
	switch r := obj.(type) {
	case *user.SignedContractRequest:
		if !r.IsNil() {
			return r.Identity
		}
	case *user.SignedJoinRequest:
		if !r.IsNil() {
			return r.Joiner
		}
	case *user.SignedExpelRequest:
		if !r.IsNil() {
			return r.Subject
		}
	}
	return nil
}
