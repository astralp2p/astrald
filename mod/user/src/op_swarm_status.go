package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSwarmStatusArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpSwarmStatus streams the status of every active node in the issuer's swarm.
// Each entry includes the node identity, its display alias, and whether it is currently linked.
func (mod *Module) OpSwarmStatus(ctx *astral.Context, q *routing.IncomingQuery, args opSwarmStatusArgs) (err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return q.RejectWithCode(2)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	for _, node := range mod.ActiveNodes(ac.Issuer) {
		alias, aliasErr := mod.Dir.GetAlias(node)
		if aliasErr != nil {
			mod.log.Error("error getting alias for node %v: %v", node, aliasErr)
		}

		err = ch.Send(&user.SwarmMember{
			Identity: node,
			Alias:    astral.String8(alias),
			Linked:   astral.Bool(mod.Nodes.IsLinked(node)),
		})
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
