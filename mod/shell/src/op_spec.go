package shell

import (
	"slices"
	"strings"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opArgsArgs struct {
	Op  string
	In  string
	Out string
}

// why: no action: the same handler serves .spec (loader.go), which both apphost
// anonymous allowlists admit. The op lists op names and arguments and changes no state.
func (mod *Module) OpSpec(ctx *astral.Context, q *routing.IncomingQuery, args opArgsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	list := mod.scopes.Spec()
	slices.SortFunc(list, func(a, b routing.OpSpec) int {
		return strings.Compare(a.Name.String(), b.Name.String())
	})

	for _, o := range list {
		if len(args.Op) > 0 && o.Name.String() != args.Op {
			continue
		}
		err = ch.Send(&o)
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
