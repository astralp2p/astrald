package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opGetAliasArgs struct {
	ID  *astral.Identity `query:"required"`
	Out string
}

// why: no action guards this op, because apps name identities under their own
// identity.
// note: astral-js exposes dir.get_alias to every app (astral-js src/api/dir/index.ts).
// note: cmd/astral-listen names each caller through it.
func (mod *Module) OpGetAlias(ctx *astral.Context, q *routing.IncomingQuery, args opGetAliasArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	alias, err := mod.GetAlias(args.ID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send((*astral.String8)(&alias))
}
