package messaging

import (
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opWaitArgs struct {
	From    string
	Since   uint64
	Timeout astral.Duration
	Out     string
}

// OpWait parks until the caller's inbox holds a message it has not archived,
// or the granted window closes, and answers one messaging.wait_result.
//
// why the query is accepted before the park: a query must be accepted or
// rejected within seconds, and a park holds for minutes.
func (mod *Module) OpWait(ctx *astral.Context, q *routing.IncomingQuery, args opWaitArgs) error {
	if !mod.admitsMailCaller(q) {
		return q.Reject()
	}

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithOutputFormat(args.Out))
	defer ch.Close()

	parkCtx, cancel := ctx.WithCancel()
	defer cancel()

	go watchClose(conn, cancel)

	res, err := mod.Wait(parkCtx, q.Caller(), messaging.WaitRequest{
		From:    args.From,
		Since:   args.Since,
		Timeout: time.Duration(args.Timeout),
	}, nil)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(res)
}

// watchClose ends the park when the caller closes its side. The caller sends
// nothing after the query, so a read returns only when the conn ends.
//
// why the park watches the conn: the op runs on a context detached from the
// caller's, so nothing else tells a park that nobody is waiting for its answer.
//
// note: after an answered park the read returns when the caller closes the
// conn, which a caller does once it holds the answer.
func watchClose(conn io.Reader, cancel func()) {
	defer cancel()
	_, _ = io.Copy(io.Discard, conn)
}
