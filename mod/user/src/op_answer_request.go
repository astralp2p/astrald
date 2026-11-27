package user

import (
	"errors"

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
	// errUnknownRequest is sent when the id names no stored request.
	errUnknownRequest = astral.NewError("unknown request")
	// errOutsideTier is sent when this node has no membership, or its tier may
	// not answer that request (user.MayAnswer).
	errOutsideTier = astral.NewError("outside this node's tier")
	// errRequestClosed is sent when the request's status admits no such
	// answer, the signer has offered for the request already, or the answer
	// approves a JoinRequest.
	errRequestClosed = astral.NewError("request is not open to this answer")
	// errInvalidAnswer is sent when the answer argument is neither approve nor
	// decline.
	errInvalidAnswer = astral.NewError("invalid answer")
)

// Words of the answer argument of user.answer_request.
const (
	answerApprove = "approve"
	answerDecline = "decline"
)

type opAnswerRequestArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Answer string           `query:"required"`
	In     string
	Out    string
}

// OpAnswerRequest answers the request whose entry ID is ID with a yes (Answer
// "approve") or a Decline (Answer "decline"), signed by this node, adds it to
// SwarmState and sends the signed answer. A yes is an offer for a ContractRequest and an Approval for an
// ExpelRequest (usermod.SwarmState.Approve). The caller needs
// AdminSwarmAction, whose Subject is the identity the request names.
// Spec: .ai/system/protocols/user/ops/user.answer_request.md
//
// It refuses, writing nothing:
//   - errInvalidAnswer when Answer is no such word;
//   - errUnknownRequest when ID names no stored request;
//   - auth.ErrNotPermitted when the caller is not authorized;
//   - errOutsideTier when this node has no membership or its tier may not
//     answer the request (user.MayAnswer);
//   - errRequestClosed when the request's status admits no such answer
//     (Module.Approve and Decline) or the merge check drops the answer.
func (mod *Module) OpAnswerRequest(ctx *astral.Context, q *routing.IncomingQuery, args opAnswerRequestArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	signed, err := mod.answerRequest(ctx, q, args)
	if err != nil {
		var aerr astral.Error
		if errors.As(err, &aerr) {
			return ch.Send(aerr)
		}
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}

// answerRequest checks, signs and writes one answer, and returns it.
func (mod *Module) answerRequest(ctx *astral.Context, q *routing.IncomingQuery, args opAnswerRequestArgs) (astral.Object, error) {
	if args.Answer != answerApprove && args.Answer != answerDecline {
		return nil, errInvalidAnswer
	}
	obj, err := mod.SwarmEntry(args.ID)
	if err != nil {
		return nil, errUnknownRequest
	}
	req, ok := swarmset.ParseRequest(obj)
	if !ok {
		return nil, errUnknownRequest
	}

	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(q.Caller()),
		Subject: req.Subject,
	}) {
		return nil, auth.ErrNotPermitted
	}

	if !mod.MayAnswer(mod.node.Identity(), args.ID) {
		return nil, errOutsideTier
	}

	// note: a held answer is signed and may still be stored after a later
	// merge, so the typed write counts it as written, and it is sent like a
	// stored one.
	var signed astral.Object
	if args.Answer == answerDecline {
		signed, err = mod.Decline(ctx, args.ID)
	} else {
		signed, err = mod.Approve(ctx, args.ID)
	}
	if errors.Is(err, usermod.ErrNotAllowed) || errors.Is(err, usermod.ErrSwarmEntryDropped) {
		return nil, errRequestClosed
	}
	return signed, err
}
