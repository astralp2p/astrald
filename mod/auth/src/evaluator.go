package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// policyConfig holds the evaluator rules, bound to the tree at /mod/auth/policy.
//
// why apart from Config: Config is read from auth.yaml, and a tree.Value
// carries a mutex, so it cannot ride a copied struct.
type policyConfig struct {
	EvaluatorRules tree.Value[*authmod.EvaluatorRules]
}

// rules returns a copy of the current rule list.
func (mod *Module) rules() []*authmod.EvaluatorRule {
	set := mod.policy.EvaluatorRules.Get()
	if set == nil {
		return nil
	}
	return append([]*authmod.EvaluatorRule(nil), set.Rules...)
}

// evaluatorRule returns the rule naming actor and actionType, if any.
func (mod *Module) evaluatorRule(actor *astral.Identity, actionType string) *authmod.EvaluatorRule {
	for _, r := range mod.rules() {
		if string(r.Action) == actionType && r.Actor.IsEqual(actor) {
			return r
		}
	}
	return nil
}

// SetEvaluatorRule replaces the rule for the rule's (Actor, Action), or removes
// it when the rule names no Evaluator.
func (mod *Module) SetEvaluatorRule(ctx *astral.Context, rule *authmod.EvaluatorRule) error {
	mod.rulesMu.Lock()
	defer mod.rulesMu.Unlock()

	var next []*authmod.EvaluatorRule
	for _, r := range mod.rules() {
		if r.Action == rule.Action && r.Actor.IsEqual(rule.Actor) {
			continue
		}
		next = append(next, r)
	}

	if rule.Evaluator != nil {
		next = append(next, rule)
	}

	return mod.policy.EvaluatorRules.Set(ctx, &authmod.EvaluatorRules{Rules: next})
}

// externalFor returns the authority that answers for actor at this level: the
// actor's evaluator rule when one names the action type, otherwise the
// config-file authorizer for the action type.
//
// why the rule wins: it is the narrower statement. The config file speaks for
// every actor of the type; the rule speaks for this one, and was set by the
// node's administrator after the file was written.
func (mod *Module) externalFor(actor *astral.Identity, actionType string) *ExternalAuthorizer {
	if r := mod.evaluatorRule(actor, actionType); r != nil {
		newAsk := mod.evaluatorAsk
		if newAsk == nil {
			newAsk = mod.astralEvaluatorAsk
		}
		return NewExternalAuthorizer(mod.log, actionType, ExternalConfig{}, newAsk(r))
	}

	ext, _ := mod.external.Get(actionType)
	return ext
}

// astralEvaluatorAsk puts the question to the rule's evaluator as an Astral
// query. The evaluator is held as an identity, so nothing is resolved.
func (mod *Module) astralEvaluatorAsk(r *authmod.EvaluatorRule) auth.AuthorizeAsk {
	query := string(r.Query)
	if query == "" {
		query = authmod.OpEvaluate
	}
	return &astralAuthorizer{
		node:   mod.node,
		name:   r.Evaluator.String(),
		path:   query,
		target: r.Evaluator,
	}
}
