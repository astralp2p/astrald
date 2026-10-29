package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterBlueprintArgs struct {
	In  string
	Out string
}

// OpRegisterBlueprint runs in batch mode: reads runtime *astral.Blueprint descriptors
// (struct kind or alias kind) from the channel until EOS or EOF, performs registration for
// each, sends the resulting ObjectID or an error per input. An explicit EOS
// input is answered with a final EOS; a stream ended by EOF is not.
//
// note: registration mutates DefaultBlueprints for the whole node — the caller squats a
// type name permanently and defines the wire schema everyone parses it with.
//
// why: a type-name squat is not recoverable the way a stored object is, so a grant of
// object writes does not carry it.
func (mod *Module) OpRegisterBlueprint(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterBlueprintArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.UseBlueprintsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	return channel.Batch(ch, func(bp *astral.Blueprint) astral.Object {
		id, regErr := mod.Register(bp)
		if regErr != nil {
			return astral.NewError(regErr.Error())
		}
		return id
	})
}
