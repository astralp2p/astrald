package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

const maxPushSize = 32 * 1024

type opPushArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpPush receives pushed objects from the caller and replies with a Bool
// per object indicating whether it was accepted.
func (mod *Module) OpPush(ctx *astral.Context, q *routing.IncomingQuery, args opPushArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Switch(
		func(o astral.Object) (err error) {
			objectID, _ := astral.ResolveObjectID(o)

			var ok = astral.Bool(mod.receive(q.Caller(), o))
			if ok {
				mod.log.Logv(1, "received %v (%v) from %v", o.ObjectType(), objectID, q.Caller())
			} else {
				mod.log.Logv(1, "rejected %v (%v) from %v", o.ObjectType(), objectID, q.Caller())
			}
			err = ch.Send(&ok)
			return
		},
		channel.BreakOnEOS,
		channel.WithContext(ctx),
	)
}
