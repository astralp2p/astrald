package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// OpSwarmPush receives one entry, merges it and answers Ack when it is stored
// or was already, or the error. Any identified caller may offer an entry.
//
// why: no named action guards this op; the entry check decides what is stored,
// whoever offers it.
//
// note: it does not use admitsSwarmCaller: two members that know nothing of each other cannot
// pull until one holds the other's membership, so each offers its own first; an
// offered entry passes the entry check like any merge, and what waits is held
// in a bounded set.
func (mod *Module) OpSwarmPush(ctx *astral.Context, q *routing.IncomingQuery) error {
	store := mod.openStore()
	if store == nil || q.Caller().IsZero() {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	obj, err := ch.Receive()
	if err != nil {
		return err
	}

	if err = store.merge(ctx, obj); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
