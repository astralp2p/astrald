package user

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// OpSwarmFetch receives entry IDs until EOS, answers each stored entry among
// them, then EOS. An ID with no stored entry is skipped. It serves the callers
// OpSwarmDigest serves, and refuses more than swarmFetchBatch IDs.
//
// why: no named action guards this op; admitsSwarmCaller names who reads
// SwarmState, as every member syncs it.
func (mod *Module) OpSwarmFetch(ctx *astral.Context, q *routing.IncomingQuery) error {
	store := mod.openStore()
	if store == nil || !mod.admitsSwarmCaller(q.Caller()) {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	var ids []*astral.ObjectID
	err := ch.Switch(
		func(id *astral.ObjectID) error {
			if len(ids) == swarmFetchBatch {
				return errors.New("too many IDs")
			}
			ids = append(ids, id)
			return nil
		},
		channel.BreakOnEOS,
		channel.FailOnEOF(),
		channel.WithContext(ctx),
	)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, id := range ids {
		obj, err := store.set.Entry(id)
		if err != nil {
			continue
		}
		if err = ch.Send(obj); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
