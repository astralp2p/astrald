package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

const maxPushSize = 32 * 1024

type opPushArgs struct {
	In  string
	Out string
}

// OpPush receives pushed objects from the caller and replies with a Bool
// per object indicating whether it was accepted.
//
// why: no action gates this op; each receiver decides what it accepts from the sender.
// A node joins another node's swarm list only through its membership contract, which
// arrives here, so a gate on swarm membership refused the one object that admits the sender.
func (mod *Module) OpPush(ctx *astral.Context, q *routing.IncomingQuery, args opPushArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(o astral.Object) astral.Object {
		objectID, _ := astral.ResolveObjectID(o)

		var ok = astral.Bool(mod.receive(q.Caller(), o))
		if ok {
			mod.log.Logv(1, "received %v (%v) from %v", o.ObjectType(), objectID, q.Caller())
		} else {
			mod.log.Logv(1, "rejected %v (%v) from %v", o.ObjectType(), objectID, q.Caller())
		}
		return &ok
	}, channel.WithContext(ctx))
}
