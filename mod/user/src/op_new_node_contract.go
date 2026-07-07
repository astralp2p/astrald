package user

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opNewNodeContractArgs struct {
	User     string `query:"optional"`
	Node     string `query:"optional"`
	Duration string `query:"optional"`
	In       string `query:"optional"`
	Out      string `query:"optional"`
}

// OpNewNodeContract constructs and returns an unsigned node contract.
// Defaults to the module's own identity for user and the local node identity for node when omitted.
// note: the contract is not signed or stored; the caller is responsible for the signing ceremony.
func (mod *Module) OpNewNodeContract(ctx *astral.Context, query *routing.IncomingQuery, args opNewNodeContractArgs) (err error) {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var userID = mod.Identity()
	if len(args.User) > 0 {
		userID, err = mod.Dir.ResolveIdentity(args.User)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	if userID.IsZero() {
		return ch.Send(astral.NewError("user id missing"))
	}

	var nodeID = mod.node.Identity()
	if len(args.Node) > 0 {
		nodeID, err = mod.Dir.ResolveIdentity(args.Node)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	if nodeID.IsZero() {
		return ch.Send(astral.NewError("node id missing"))
	}

	var duration = defaultContractValidity
	if len(args.Duration) > 0 {
		duration, err = time.ParseDuration(args.Duration)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	// the setup ceremony provisions the user's primary node, which manages the swarm
	contract, err := user.NewNodeContract(userID, nodeID, true, duration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(contract)
}
