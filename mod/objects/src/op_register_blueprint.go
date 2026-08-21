package objects

import (
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
// type name permanently and defines the wire schema everyone parses it with. StoreObjects
// is the gate. The type name is not a noun here: the blueprints arrive on the channel,
// after the authorization decision.
//
// todo: a permanent type-name squat is not recoverable the way a stored object is, so a
// grantor handing out StoreObjects would not expect it. Splitting this op into its own
// action costs one action type and one call site.
func (mod *Module) OpRegisterBlueprint(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterBlueprintArgs) error {
	if !mod.authorizeStoreObjects(ctx, q, "", "") {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(bp *astral.Blueprint) astral.Object {
		id, regErr := mod.Register(bp)
		if regErr != nil {
			return astral.NewError(regErr.Error())
		}
		return id
	})
}
