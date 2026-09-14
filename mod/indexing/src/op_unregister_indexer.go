package indexing

import (
	"github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opUnregisterIndexerArgs struct {
	Nonce astral.Nonce `query:"required"`
	In    string
	Out   string
}

// OpUnregisterIndexer deletes a registration the caller owns, or any
// registration when the caller holds AdminObjects.
func (mod *Module) OpUnregisterIndexer(ctx *astral.Context, q *routing.IncomingQuery, args opUnregisterIndexerArgs) error {
	// why: cheapest refusal first. Indexing state is this node's own, so a network
	// caller is refused whatever it holds.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	idxer, err := mod.findIndexerByNonce(ctx, args.Nonce)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: a registration the caller may not delete answers as a missing one, so a
	// caller cannot probe which nonces are registered.
	if idxer == nil || !mod.mayUnregister(ctx, q, idxer) {
		return ch.Send(astral.Err(indexing.ErrIndexNotFound))
	}

	err = mod.UnregisterIndexer(ctx, args.Nonce)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// mayUnregister reports whether the query's caller may delete idxer.
//
// why: the owner needs no permit, so an identity whose indexer grant is revoked
// still deletes its own registration.
func (mod *Module) mayUnregister(ctx *astral.Context, q *routing.IncomingQuery, idxer *indexerHandle) bool {
	return idxer.ownedBy(q.Caller()) || mod.authorizeAdminObjects(ctx, q)
}
