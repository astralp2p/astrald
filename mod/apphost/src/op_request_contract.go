package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

type opRequestContractArgs struct {
	In  string
	Out string
}

// requestContract is apphost.request_contract. It reads mod.auth.permit
// objects until EOS, files a ContractRequest asking them for the calling
// session's identity (see contractRequests.File), and sends the request's
// entry ID. It then waits while the caller stays connected and sends the
// contract it signs once it co-signed an offer for the request, or the error of a
// Decline or an expiry. A caller that disconnects calls again with the same
// permits: the live request is not filed twice, and its outcome is sent at
// once when it has one.
//
// why not OpRequestContract: Load registers every Op-prefixed method, and
// G7 registers this op when it builds contractRequests.
// why the session and not q.Caller(): the request names the app that holds
// the token, as apphost.request_grant does.
func (mod *Module) requestContract(ctx *astral.Context, q *routing.IncomingQuery, args opRequestContractArgs) error {
	// why before accepting: accepting drops the en-route entry naming the session.
	requester := mod.sessionOwner(q)

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	switch {
	case q.Origin() == astral.OriginNetwork:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	case requester.IsZero():
		return ch.Send(auth.ErrNotPermitted)
	case mod.contractRequests == nil:
		return ch.Send(astral.Err(apphostmod.ErrContractRequestsOff))
	}

	permits, err := receivePermits(ch)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	reqID, err := mod.contractRequests.File(ctx, requester, permits)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if err = ch.Send(reqID); err != nil {
		return err
	}

	// why: an op runs on a context detached from the requester's, and only the
	// conn tells the wait that nobody reads its answer.
	waitCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	contract, err := mod.contractRequests.Wait(waitCtx, reqID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	return ch.Send(contract)
}

// receivePermits reads mod.auth.permit objects from ch until EOS.
func receivePermits(ch *channel.Channel) ([]*auth.Permit, error) {
	var permits []*auth.Permit
	var sawEOS bool
	err := ch.Switch(channel.Collect(&permits), channel.MarkEOS(&sawEOS), channel.FailOnEOF())
	switch {
	case err != nil:
		return nil, err
	case !sawEOS:
		return nil, errors.New("permits not terminated by EOS")
	}
	return permits, nil
}
