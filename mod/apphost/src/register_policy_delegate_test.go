package apphost

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/apphost"
)

// The request and decision cross to the delegate as objects, so both must come
// back from the wire with every permit on the rail it left on.
func TestAppRegisterObjectsSurviveTheWire(t *testing.T) {
	req := &apphost.AppRegisterRequest{
		Origin:          "https://app.example",
		GrantPermits:    parsePermits("mod.auth.serve_objects_action"),
		ContractPermits: parsePermits("mod.nodes.relay_for_action,mod.auth.see_objects_action"),
	}

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, req); err != nil {
		t.Fatal(err)
	}
	got, err := astral.DecodeAs[*apphost.AppRegisterRequest](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != req.Origin || !sameActions(got.GrantPermits, req.GrantPermits) || !sameActions(got.ContractPermits, req.ContractPermits) {
		t.Fatalf("request changed on the wire: %+v", got)
	}

	dec := &apphost.AppRegisterDecision{Allow: true, ContractPermits: parsePermits("mod.nodes.relay_for_action")}

	buf.Reset()
	if _, err := astral.Encode(&buf, dec); err != nil {
		t.Fatal(err)
	}
	gotDec, err := astral.DecodeAs[*apphost.AppRegisterDecision](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bool(gotDec.Allow) || len(gotDec.GrantPermits) != 0 || !sameActions(gotDec.ContractPermits, dec.ContractPermits) {
		t.Fatalf("decision changed on the wire: %+v", gotDec)
	}
}

// With no delegate set, registration keeps today's accept-all behavior.
func TestNoDelegateKeepsAcceptAll(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	grants, contracts, ok := mod.GetAppRegisterPolicy()(nil, "", parsePermits("mod.auth.serve_objects_action"), nil)
	if !ok || len(grants) != 1 || len(contracts) != 0 {
		t.Fatalf("got grants=%v contracts=%v ok=%v, want the request back", actions(grants), actions(contracts), ok)
	}
}

// A delegated policy reached without a delegate refuses rather than admits.
func TestDelegatedPolicyWithoutDelegateRefuses(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	if _, _, ok := mod.AppRegisterViaDelegate(nil, "", parsePermits("mod.auth.serve_objects_action"), nil); ok {
		t.Fatal("a delegated policy with no delegate admitted the registration")
	}
}
