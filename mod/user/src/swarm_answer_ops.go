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
	// ErrRequestClosed: the request's status admits no such answer, the
	// signer has offered for the request already, or the answer approves a
	// JoinRequest.
	ErrRequestClosed = astral.NewError("request is not open to this answer")
	// ErrInvalidAnswer: the answer argument is neither approve nor decline, or
	// the as argument is neither node nor user.
	ErrInvalidAnswer = astral.NewError("invalid answer")
)

// Words of the answer and as arguments of user.answer_request.
const (
	answerApprove = "approve"
	answerDecline = "decline"
	answerAsNode  = "node"
	answerAsUser  = "user"
)

// answerer signs and adds an answer: the typed writes of usermod.SwarmState.
type answerer interface {
	Approve(ctx *astral.Context, request *astral.ObjectID) (astral.Object, error)
	Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error)
}

// answerState is the part of usermod.SwarmState the answering ops use; its
// writes sign as this node.
type answerState interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
	SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error)
	answerer
}

// swarmAuthorizer is the part of the auth module the answering ops use.
type swarmAuthorizer interface {
	Authorize(ctx *astral.Context, action auth.ActionObject) bool
}

// SwarmAnswerOps serves user.swarm_requests and user.answer_request: it lists
// the requests in SwarmState with their status, and answers one with an
// offer, an Approval or a Decline signed by this node, only within this
// node's tier, or by the user key.
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
	// AsUser returns the typed writes that sign with the ceremony's user key,
	// for user.answer_request with as=user; nil: the user key answers
	// nothing.
	AsUser func(uk *userKeyCeremony) answerer
}

type opSwarmRequestsArgs struct {
	All bool
	Out string
}

// OpSwarmRequests authorizes the caller under SeeSwarm (Members may see
// requests), then streams the requests stored in SwarmState, oldest filed
// first. Without All it lists only the open requests, pending or offered. For each request it sends
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

// listRequests returns the stored requests, only the open ones (pending or
// offered) unless all is set, oldest filed first; requests filed at the same
// time sort by ID.
func (ops *SwarmAnswerOps) listRequests(all bool) ([]listedRequest, error) {
	ids, err := ops.State.SwarmEntries(user.EntryRequests)
	if err != nil {
		return nil, err
	}

	var list []listedRequest
	for _, id := range ids {
		status := ops.View.RequestStatus(id)
		if !all && status != user.RequestPending && status != user.RequestOffered {
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
	As     string
	In     string
	Out    string
}

// OpAnswerRequest answers the request whose entry ID is ID with a yes (Answer
// "approve") or a Decline (Answer "decline"), signed by this node, or by the
// user key when As is "user", adds it to SwarmState and sends the signed
// answer. A yes is an offer for a ContractRequest and an Approval for an
// ExpelRequest (usermod.SwarmState.Approve). The caller needs
// AdminSwarmAction, whose Subject is the identity the request names.
// Spec: .ai/system/protocols/user/ops/user.answer_request.md
//
// It refuses, writing nothing:
//   - ErrInvalidAnswer when Answer or As is no such word;
//   - astral.ErrOriginNotAllowed when As is "user" and the query is not
//     local, since a node relaying as the user reaches this node with the
//     user as caller;
//   - ErrNoUserKey when As is "user" and this node cannot reach the user key;
//   - ErrUnknownRequest when ID names no stored request;
//   - auth.ErrNotPermitted when the caller is not authorized, or As is "user"
//     and the caller is not the user;
//   - ErrOutsideTier when this node has no membership or its tier may not
//     answer the request (user.MayAnswer);
//   - ErrRequestClosed when the request admits no such answer
//     (answerOpen).
func (ops *SwarmAnswerOps) OpAnswerRequest(ctx *astral.Context, q *routing.IncomingQuery, args opAnswerRequestArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	signed, err := ops.answer(ctx, q, args)
	if err != nil {
		var aerr astral.Error
		if errors.As(err, &aerr) {
			return ch.Send(aerr)
		}
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}

// answerSigner is who signs an answer, and the writes that sign as it.
type answerSigner struct {
	id     *astral.Identity
	writes answerer
	// user is the user key's ceremony when the user key signs; nil for this node.
	user *userKeyCeremony
}

// signer returns who signs for the As argument as: this node, or the user key
// for a local query.
func (ops *SwarmAnswerOps) signer(q *routing.IncomingQuery, as string) (answerSigner, error) {
	switch as {
	case "", answerAsNode:
		return answerSigner{id: ops.Node, writes: ops.State}, nil
	case answerAsUser:
	default:
		return answerSigner{}, ErrInvalidAnswer
	}
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return answerSigner{}, astral.NewErrOriginNotAllowed(q.Origin())
	}
	var uk *userKeyCeremony
	if ops.UserKey != nil {
		uk = ops.UserKey()
	}
	if uk == nil || ops.AsUser == nil {
		return answerSigner{}, ErrNoUserKey
	}
	return answerSigner{id: uk.user, writes: ops.AsUser(uk), user: uk}, nil
}

// answer checks, signs and writes one answer, and returns it.
func (ops *SwarmAnswerOps) answer(ctx *astral.Context, q *routing.IncomingQuery, args opAnswerRequestArgs) (astral.Object, error) {
	if args.Answer != answerApprove && args.Answer != answerDecline {
		return nil, ErrInvalidAnswer
	}
	signer, err := ops.signer(q, args.As)
	if err != nil {
		return nil, err
	}

	obj, err := ops.State.SwarmEntry(args.ID)
	if err != nil {
		return nil, ErrUnknownRequest
	}
	req, ok := swarmset.ParseRequest(obj)
	if !ok {
		return nil, ErrUnknownRequest
	}

	if !ops.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(q.Caller()),
		Subject: req.Subject,
	}) {
		return nil, auth.ErrNotPermitted
	}
	// why caller and not Auth: as with user.confirm_join, the user key signs
	// only on the user's own action, which no grant stands in for.
	if signer.user != nil && !q.Caller().IsEqual(signer.user.user) {
		return nil, auth.ErrNotPermitted
	}

	if signer.user == nil && !ops.View.MayAnswer(ops.Node, args.ID) {
		return nil, ErrOutsideTier
	}
	if !ops.answerOpen(args.ID, obj, args.Answer, signer.id) {
		return nil, ErrRequestClosed
	}

	// note: a held answer is signed and may still be stored after a later
	// merge, so the typed write counts it as written, and it is sent like a
	// stored one.
	if args.Answer == answerDecline {
		return signer.writes.Decline(ctx, args.ID)
	}
	return signer.writes.Approve(ctx, args.ID)
}

// answerOpen reports whether the request id, stored as obj, admits answer
// signed by signer:
//   - a Decline: a pending, offered or approved request;
//   - an offer of a ContractRequest: a pending or offered one, which signer
//     has not offered for yet; the merge check drops a second offer;
//   - an Approval of an ExpelRequest: a pending one;
//   - nothing approves a JoinRequest: its carrier signs the membership.
//
// Nobody answers a fulfilled, expired or declined request.
func (ops *SwarmAnswerOps) answerOpen(id *astral.ObjectID, obj astral.Object, answer string, signer *astral.Identity) bool {
	status := ops.View.RequestStatus(id)
	if answer == answerDecline {
		return status == user.RequestPending || status == user.RequestOffered || status == user.RequestApproved
	}
	switch req := obj.(type) {
	case *user.SignedContractRequest:
		return (status == user.RequestPending || status == user.RequestOffered) && !offeredBy(ops.View, id, req, signer)
	case *user.SignedExpelRequest:
		return status == user.RequestPending
	}
	return false
}

// offeredBy reports whether a stored contract from signer to req's filer, an
// offer or its completed form, names the request id.
func offeredBy(view usermod.SwarmView, id *astral.ObjectID, req *user.SignedContractRequest, signer *astral.Identity) bool {
	for _, c := range view.ContractsOf(id) {
		if !c.IsNil() && c.Issuer.IsEqual(signer) && c.Subject.IsEqual(req.FiledBy) {
			return true
		}
	}
	return false
}
