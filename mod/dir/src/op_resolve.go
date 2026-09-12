package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opResolveArgs struct {
	Name string `query:"required"`
	Out  string
}

// why: no action guards this op, because apps resolve names under their own
// identity.
// note: contacts resolves localuser this way (contacts app/user.go:45).
// note: astral-js exposes dir.resolve to every app (astral-js src/api/dir/index.ts).
func (mod *Module) OpResolve(ctx *astral.Context, q *routing.IncomingQuery, args opResolveArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	id, err := mod.ResolveIdentity(args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(id)
}
