package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListEvaluatorsArgs struct {
	Out string
}

// OpListEvaluators sends every evaluator rule, then EOS.
func (mod *Module) OpListEvaluators(ctx *astral.Context, q *routing.IncomingQuery, args opListEvaluatorsArgs) error {
	// why: the rule set is this node's private answer about local authority, so
	// it is not enumerable over a link.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, r := range mod.rules() {
		if err := ch.Send(r); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
