package apphost

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// OpDecideAppRegister is the op an app register delegate serves. The node
// sends it one AppRegisterRequest and reads one AppRegisterDecision back.
const OpDecideAppRegister = "apphost.decide_app_register"

// ErrRegistrationDeclined is sent to a registering app the policy refused.
var ErrRegistrationDeclined = astral.NewError("registration declined")

// AppRegisterRequest is what the node asks the app register delegate: who is
// registering, the registering origin, and the permits the app requested on
// each rail, with the origin's trusted-source entitlement already joined onto
// the contract rail.
//
// Caller is the identity that called apphost.register. Anonymous is true when
// that caller's apphost session presented no token; the core router then
// substitutes this node's identity for the missing caller, so Caller alone
// cannot tell an anonymous local process from the node.
type AppRegisterRequest struct {
	Origin          astral.String8
	GrantPermits    []*auth.Permit
	ContractPermits []*auth.Permit
	Caller          *astral.Identity
	Anonymous       astral.Bool
}

func (AppRegisterRequest) ObjectType() string { return "mod.apphost.app_register_request" }

func (r AppRegisterRequest) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&r).WriteTo(w)
}

func (r *AppRegisterRequest) ReadFrom(rd io.Reader) (n int64, err error) {
	return astral.Objectify(r).ReadFrom(rd)
}

// AppRegisterDecision is the delegate's answer. Allow false refuses the
// registration; otherwise the node writes exactly the permits listed, on the
// rail each list names, and sets each evaluator rule for the new identity.
//
// Evaluators leave Actor empty: the app's identity is generated after the
// decision, and registration fills it in.
type AppRegisterDecision struct {
	Allow           astral.Bool
	GrantPermits    []*auth.Permit
	ContractPermits []*auth.Permit
	Evaluators      []*authmod.EvaluatorRule
}

func (AppRegisterDecision) ObjectType() string { return "mod.apphost.app_register_decision" }

func (d AppRegisterDecision) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&d).WriteTo(w)
}

func (d *AppRegisterDecision) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(d).ReadFrom(r)
}

func init() {
	astral.MustAdd(&AppRegisterRequest{})
	astral.MustAdd(&AppRegisterDecision{})
}
