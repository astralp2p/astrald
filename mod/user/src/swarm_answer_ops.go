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
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
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

// answerState is the part of usermod.SwarmState the answering ops use.
type answerState interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
	SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error)
	Approve(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error)
	Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error)
	AddContract(ctx *astral.Context, sc *auth.SignedContract) error
}

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
// It writes SwarmState only through its typed writes, which sign as this
// node, and reads what entries mean only through user.SwarmView. wireSwarm
// adds it to the module's router with AddStructPrefix(ops, "Op").
type SwarmAnswerOps struct {
	Node  *astral.Identity
	State answerState
	View  usermod.SwarmView
	Auth  swarmAuthorizer
	// UserKey returns the ceremony that signs for user.confirm_join; nil:
	// this node cannot reach the user key, and user.confirm_join refuses.
	UserKey func() *userKeyCeremony
	// Confirmed keeps what user.confirm_join signed for the carrier of the
	// join (swarmCarrier.awaitConfirmation).
	Confirmed *joinConfirmations
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
		r, ok := swarmset.ParseRequest(obj)
		if !ok {
			continue
		}
		list = append(list, listedRequest{id: id, status: status, req: obj, filedAt: r.FiledAt.Time()})
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

	obj, err := ops.State.SwarmEntry(id)
	if err != nil {
		return nil, ErrUnknownRequest
	}
	req, ok := swarmset.ParseRequest(obj)
	if !ok {
		return nil, ErrUnknownRequest
	}

	if !ops.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(caller),
		Subject: req.Subject,
	}) {
		return nil, auth.ErrNotPermitted
	}

	if !ops.View.MayAnswer(ops.Node, id) {
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

	// note: a held answer is signed and may still be stored after a later
	// merge, so the typed write counts it as written, and it is sent like a
	// stored one.
	if answer == answerDecline {
		d, err := ops.State.Decline(ctx, id)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
	a, err := ops.State.Approve(ctx, id)
	if err != nil {
		return nil, err
	}
	return a, nil
}
