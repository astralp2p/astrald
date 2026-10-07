package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

type opSetEvaluatorArgs struct {
	// Actor is the identity or alias whose actions the evaluator decides.
	Actor string `query:"required"`
	// Action is the action object type, e.g. mod.auth.see_objects_action.
	Action string `query:"required"`
	// Evaluator is the identity or alias that decides. Empty removes the rule.
	Evaluator string
	// Query is the op the evaluator serves. Empty means auth.evaluate.
	Query string
	In    string
	Out   string
}

// OpSetEvaluator sets or removes the evaluator rule for one actor and action
// type. The rule takes effect on the next authorization.
//
// why AdminManageApps: a rule decides what an app may do on this node, which is
// the administration of app authority that action covers.
func (mod *Module) OpSetEvaluator(ctx *astral.Context, q *routing.IncomingQuery, args opSetEvaluatorArgs) error {
	// why: the rule set is this node's setting, so only a caller on this node has
	// standing to write it. Origins are admitted by name, so one added later is
	// refused until it is judged.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	if !mod.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	actor, err := mod.Dir.ResolveIdentity(args.Actor)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if actor.IsZero() {
		return ch.Send(astral.NewError("missing actor"))
	}

	rule := &authmod.EvaluatorRule{
		Actor:  actor,
		Action: astral.String8(args.Action),
		Query:  astral.String8(args.Query),
	}

	if args.Evaluator == "" {
		if err = mod.setEvaluatorRule(ctx, rule); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "evaluator rule for %v %v removed", actor, args.Action)
		return ch.Send(&astral.Ack{})
	}

	evaluator, err := mod.Dir.ResolveIdentity(args.Evaluator)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if evaluator.IsZero() {
		return ch.Send(astral.NewError("missing evaluator"))
	}

	// why: an evaluator asked whether it may act would have to answer the
	// question before it could answer the question; the asker refuses this at
	// read time, so the rule could never allow.
	if evaluator.IsEqual(actor) {
		return ch.Send(astral.NewError("the evaluator cannot be the actor"))
	}

	rule.Evaluator = evaluator
	if err = mod.setEvaluatorRule(ctx, rule); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "evaluator rule for %v %v set to %v", actor, args.Action, evaluator)

	return ch.Send(&astral.Ack{})
}
