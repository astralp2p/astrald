package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSignContractArgs struct {
	In  string
	Out string
}

// OpSignContract reads contracts from the channel, signs each as its issuer and
// as its subject with keys the node holds, and writes back a SignedContract or
// an error per contract. A query of any origin other than local, a link's and
// MCP's included, is rejected.
//
// note: a command of a shell.shell session carries no origin, so a remote
// session's command passes the origin check; the signer rule still binds the
// session's identity.
func (mod *Module) OpSignContract(ctx *astral.Context, q *routing.IncomingQuery, args opSignContractArgs) error {
	// why: the origin is admitted by name rather than refused by name, so an
	// origin added later (as mcp once was) is refused until it is judged.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(c *auth.Contract) astral.Object {
		// why: authorized per contract and before any key is touched, because
		// each contract names its own parties.
		if err := mod.authorizeContract(ctx, q.Caller(), c); err != nil {
			return astral.Err(err)
		}

		signed := &auth.SignedContract{Contract: c}
		if err := mod.SignContract(ctx, signed); err != nil {
			return astral.Err(err)
		}
		return signed
	})
}
