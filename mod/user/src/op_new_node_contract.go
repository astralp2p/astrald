package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	usermod "github.com/astralp2p/astrald/mod/user"
)

type opNewNodeContractArgs struct {
	User     string
	Node     string
	Duration string
	In       string
	Out      string
}

// OpNewNodeContract constructs and returns an unsigned node contract.
// Defaults to the module's own identity for user and the local node identity for node when omitted.
// note: the contract is not signed or stored; the caller is responsible for the signing ceremony.
func (mod *Module) OpNewNodeContract(ctx *astral.Context, query *routing.IncomingQuery, args opNewNodeContractArgs) (err error) {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: no network caller needs the node to build a contract; a remote peer
	// builds the same object client-side with user.NewNodeContract.
	if query.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(query.Origin()))
	}

	// why: SeeSwarm on the default user mirrors user.info, which guards the active
	// contract's issuer. It hides nothing: dir.resolve answers localuser with that
	// issuer to any app (identity_resolver.go), so a named user needs no action.
	var userID = mod.Identity()
	if len(args.User) > 0 {
		userID, err = mod.Dir.ResolveIdentity(args.User)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	} else if !userID.IsZero() && !mod.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(query.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if userID.IsZero() {
		if len(args.User) == 0 {
			return ch.Send(astral.NewError("user id missing: pass -user <identity>; the node has no active contract to default from"))
		}
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

	var duration = usermod.DefaultContractValidity
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
