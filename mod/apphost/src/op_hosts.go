package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opHostsArgs struct {
	App string `query:"required"`
	Out string
}

// OpHosts sends each node that hosts the app once, as an astral.Identity, then EOS.
// An app with no known host, or a name that resolves to no app, gets EOS alone.
// A host is the subject of an active relay-for contract the app issued, the same lookup
// PreprocessQuery routes by (query_preprocessor.go), with this node included when it hosts the app.
// note: a sibling's app is known once its relay contract arrived: at the first link,
// at the app's registration, or when the app runs swarm discovery.
func (mod *Module) OpHosts(ctx *astral.Context, q *routing.IncomingQuery, args opHostsArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why: the answer names the nodes of the caller's swarm, so it is not
	// enumerable over a link.
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	// why SeeSwarm: the answer is which swarm nodes run an app, a read of who
	// belongs to the swarm.
	if !mod.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: a name that resolves to no app names an app with no known host, and a
	// caller that reads hosts until EOS gets an empty list, not an error.
	app, err := mod.Dir.ResolveIdentity(args.App)
	if err != nil || app.IsZero() {
		return ch.Send(&astral.EOS{})
	}

	contracts, err := mod.Auth.SignedContracts().
		WithIssuer(app).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx.ExcludeZone(astral.ZoneNetwork))
	if err != nil {
		mod.log.Errorv(1, "error finding hosts of %v: %v", app, err)
		return ch.Send(astral.Err(err))
	}

	// why: an app may hold several active relay contracts with one node, and a
	// host is named once.
	sent := map[string]bool{}
	for _, contract := range contracts {
		host := contract.Subject
		if host.IsZero() || sent[host.String()] {
			continue
		}
		sent[host.String()] = true

		if err = ch.Send(host); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
