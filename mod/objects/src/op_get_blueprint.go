package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opGetBlueprintArgs struct {
	Type string
	Out  string `query:"optional"`
}

// OpGetBlueprint sends the Blueprint for a single type name. Referenced types are not
// resolved or included — the caller fetches them itself. Primitive types have no
// blueprint and return an error.
func (mod *Module) OpGetBlueprint(ctx *astral.Context, q *routing.IncomingQuery, args opGetBlueprintArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	bp, err := mod.GetBlueprint(args.Type)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(bp)
}
