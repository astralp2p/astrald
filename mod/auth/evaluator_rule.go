package auth

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// OpEvaluate is the op an evaluator serves when a rule names no other. The node
// sends it the action object and reads one object back; only an astral.Ack
// allows.
const OpEvaluate = "auth.evaluate"

// EvaluatorRule names the identity that decides one action type for one actor,
// after the registered handlers and the contract chain have not allowed it.
//
// why keyed by actor as well as action: the evaluator speaks for the apps the
// user assigned to it, not for every caller of the action type. A config-file
// external authorizer keeps answering for every other actor.
type EvaluatorRule struct {
	Actor     *astral.Identity
	Action    astral.String8
	Evaluator *astral.Identity
	// Query is the op the evaluator serves. Empty means OpEvaluate.
	Query astral.String8
}

func (EvaluatorRule) ObjectType() string { return "mod.auth.evaluator_rule" }

func (r EvaluatorRule) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&r).WriteTo(w)
}

func (r *EvaluatorRule) ReadFrom(rd io.Reader) (n int64, err error) {
	return astral.Objectify(r).ReadFrom(rd)
}

// EvaluatorRules is the node's evaluator rule set, held as one tree value so a
// configuring app follows a single path.
type EvaluatorRules struct {
	Rules []*EvaluatorRule
}

func (EvaluatorRules) ObjectType() string { return "mod.auth.evaluator_rules" }

func (r EvaluatorRules) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&r).WriteTo(w)
}

func (r *EvaluatorRules) ReadFrom(rd io.Reader) (n int64, err error) {
	return astral.Objectify(r).ReadFrom(rd)
}

func init() {
	astral.MustAdd(&EvaluatorRule{})
	astral.MustAdd(&EvaluatorRules{})
}
