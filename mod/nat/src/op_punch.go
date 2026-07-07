package nat

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	natclient "github.com/cryptopunkscc/astrald/mod/nat/client"
)

type opPunchArgs struct {
	Target string
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

// OpPunch drives the initiator side of the NAT punch protocol and registers the resulting hole.
func (mod *Module) OpPunch(ctx *astral.Context, q *routing.IncomingQuery, args opPunchArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out), channel.WithInputFormat(args.In))
	defer ch.Close()

	localIP, err := mod.getLocalIPv4()
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	target, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Log("starting traversal as initiator to %v", target)
	puncher, err := mod.newPuncher(nil)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	client := natclient.New(target, astrald.Default())
	hole, err := client.NodePunch(ctx, target, localIP, puncher)
	if err != nil {
		mod.log.Error("NAT traversal failed with %v: %v", target, err)
		puncher.Close()
		return ch.Send(astral.Err(err))
	}

	puncher.Close()

	mod.log.Info("NAT traversal succeeded with %v: %v <-> %v", target, hole.ActiveEndpoint, hole.PassiveEndpoint)

	mod.addHole(*hole, true)
	return ch.Send(hole)
}
