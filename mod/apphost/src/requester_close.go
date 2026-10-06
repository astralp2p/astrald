package apphost

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// watchRequester returns a context that ends when the requester's side of conn
// ends, and a stop func the op defers. It suits ops whose requester sends
// nothing more by the time the watch starts, so a read returns only when the
// requester closes its side.
//
// why: an op runs on a context detached from the requester's, so nothing else
// tells a waiting decision that nobody waits for its answer.
//
// why the underlying reader: routing.Conn.Read closes the reply direction on
// any read error, so watching through it would cut the answer off from a
// requester that only closed its write side. Reading the pipe beneath only
// observes the end.
//
// note: a requester that closes its write side (half-close) before the answer
// arrives counts as having left. stop closes the read side, which ends the
// watch when the op returns before the requester closes.
func watchRequester(ctx *astral.Context, conn io.ReadWriteCloser) (*astral.Context, func()) {
	watchCtx, cancel := ctx.WithCancel()

	var r io.Reader = conn
	var readSide io.Closer
	if rc, ok := conn.(*routing.Conn); ok {
		r = rc.Reader
		readSide, _ = rc.Reader.(io.Closer)
	}

	go func() {
		defer cancel()
		_, _ = io.Copy(io.Discard, r)
	}()

	return watchCtx, func() {
		cancel()
		if readSide != nil {
			_ = readSide.Close()
		}
	}
}
