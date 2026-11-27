package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// OpSwarmDigest answers the ID of every stored entry, then EOS. It serves this
// node, the root key and every identity holding a membership
// (admitsSwarmCaller); it rejects any other caller, and every caller while the
// store is closed.
//
// why: no named action guards this op; admitsSwarmCaller names who reads
// SwarmState, as every member syncs it.
func (mod *Module) OpSwarmDigest(ctx *astral.Context, q *routing.IncomingQuery) error {
	store := mod.openStore()
	if store == nil || !mod.admitsSwarmCaller(q.Caller()) {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	for _, id := range store.set.Digest() {
		if err := ch.Send(id); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
