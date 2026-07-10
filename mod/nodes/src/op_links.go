package nodes

import (
	"slices"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opLinksArgs struct {
	Out string `query:"optional"`
}

// OpLinks lists all links.
func (mod *Module) OpLinks(ctx *astral.Context, q *routing.IncomingQuery, args opLinksArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	links := mod.linkPool.links.Clone()

	slices.SortFunc(links, func(a, b *Link) int {
		return a.createdAt.Compare(b.createdAt)
	})

	for _, s := range links {
		err = ch.Send(&nodes.LinkInfo{
			ID:              s.id,
			LocalIdentity:   s.LocalIdentity(),
			RemoteIdentity:  s.RemoteIdentity(),
			LocalEndpoint:   s.LocalEndpoint(),
			RemoteEndpoint:  s.RemoteEndpoint(),
			Outbound:        astral.Bool(s.outbound),
			Network:         astral.String8(s.Network()),
			HighPressure:    astral.Bool(s.PressureHigh()),
			BytesThroughput: astral.Uint64(s.Throughput()),
		})
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
