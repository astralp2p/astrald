package shell

import (
	"slices"
	"strings"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opArgsArgs struct {
	Op  string `query:"optional"`
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpSpec(ctx *astral.Context, q *routing.IncomingQuery, args opArgsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	list := mod.scopes.Spec()
	slices.SortFunc(list, func(a, b routing.OpSpec) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, o := range list {
		if len(args.Op) > 0 && o.Name != args.Op {
			continue
		}
		err = ch.Send(&o)
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
