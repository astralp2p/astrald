package apphost

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opHostsArgs struct {
	App string `query:"required"`
	Out string
}

// OpHosts sends each node that hosts the app once, as an astral.Identity, then EOS.
// An app with no known host gets EOS alone.
// A host is the subject of an active relay-for contract the app issued, the same lookup
// PreprocessQuery routes by (query_preprocessor.go), with this node included when it hosts the app.
// note: a sibling's app is known once its relay contract arrived, at the first link or at registration.
func (mod *Module) OpHosts(ctx *astral.Context, q *routing.IncomingQuery, args opHostsArgs) error {
	// why: the answer names the nodes of the caller's swarm, so it is not
	// enumerable over a link.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	// why no permit: swarm discovery already shows a local caller the swarm's
	// apps, and this op reads nothing beyond where they run.
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	app, err := mod.Dir.ResolveIdentity(args.App)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if app.IsZero() {
		return ch.Send(astral.NewError("missing app"))
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
