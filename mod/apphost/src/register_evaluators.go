package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// setRegisteredEvaluators sets each evaluator rule a registration policy named,
// with the newly generated identity as its actor.
//
// why the actor is filled here: the identity is generated after the policy
// decides, so a policy can name the evaluator and the action but not the app.
func (mod *Module) setRegisteredEvaluators(ctx *astral.Context, guestID *astral.Identity, rules []*authmod.EvaluatorRule) error {
	for _, r := range rules {
		switch {
		case r == nil:
			return errors.New("evaluator rule: nil")
		case r.Evaluator == nil || r.Evaluator.IsZero():
			return errors.New("evaluator rule: no evaluator named")
		case r.Action == "":
			return errors.New("evaluator rule: no action named")
		}

		rule := *r
		rule.Actor = guestID
		if err := mod.Auth.SetEvaluatorRule(ctx, &rule); err != nil {
			return err
		}
		mod.log.Logv(1, "registered %v: %v evaluated by %v", guestID, rule.Action, rule.Evaluator)
	}
	return nil
}
