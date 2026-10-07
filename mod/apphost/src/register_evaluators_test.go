package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// ruleRecorder records the evaluator rules registration sets.
//
// why: the embedded nil interface satisfies authmod.Module; only
// SetEvaluatorRule is reached.
type ruleRecorder struct {
	authmod.Module
	set []*authmod.EvaluatorRule
}

func (r *ruleRecorder) SetEvaluatorRule(_ *astral.Context, rule *authmod.EvaluatorRule) error {
	r.set = append(r.set, rule)
	return nil
}

// A policy names the evaluator and the action; registration names the app.
func TestRegisteredEvaluatorsNameTheNewIdentity(t *testing.T) {
	rec := &ruleRecorder{}
	mod := &Module{log: log.New(nil)}
	mod.Auth = rec
	guest, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	asked := &authmod.EvaluatorRule{Action: "mod.auth.see_objects_action", Evaluator: media}
	if err := mod.setRegisteredEvaluators(nil, guest, []*authmod.EvaluatorRule{asked}); err != nil {
		t.Fatalf("set: %v", err)
	}

	if len(rec.set) != 1 {
		t.Fatalf("set %d rules; want 1", len(rec.set))
	}
	got := rec.set[0]
	if !got.Actor.IsEqual(guest) || !got.Evaluator.IsEqual(media) || got.Action != asked.Action {
		t.Fatalf("set %+v; want actor %v evaluated by %v", got, guest, media)
	}
	if asked.Actor != nil {
		t.Fatal("the policy's rule was changed in place")
	}
}

// A rule naming no evaluator or no action is refused, so a registration never
// writes a rule that cannot decide.
func TestRegisteredEvaluatorsRefuseAnIncompleteRule(t *testing.T) {
	for name, rule := range map[string]*authmod.EvaluatorRule{
		"nil":          nil,
		"no evaluator": {Action: "mod.auth.see_objects_action"},
		"no action":    {Evaluator: astral.GenerateIdentity()},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &ruleRecorder{}
			mod := &Module{log: log.New(nil)}
			mod.Auth = rec

			if err := mod.setRegisteredEvaluators(nil, astral.GenerateIdentity(), []*authmod.EvaluatorRule{rule}); err == nil {
				t.Fatal("an incomplete rule was accepted")
			}
			if len(rec.set) != 0 {
				t.Fatal("an incomplete rule was written")
			}
		})
	}
}
