package ip

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opLocalAddrsArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpLocalAddrs(ctx *astral.Context, q *routing.IncomingQuery, args opLocalAddrsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	addrs, err := mod.localAddresses(false)
	if err != nil {
		return
	}

	for _, addr := range addrs {
		err = ch.Send(&addr)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
