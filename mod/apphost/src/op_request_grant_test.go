package apphost

import (
	"bytes"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

const grantRequestOp = "control.decide_grant_request"

// ackAnswer is what answer returns for an ack.
var ackAnswer = (&astral.Ack{}).ObjectType()

// grantRequester is a module that records grants in memory and delegates grant
// requests to node.
func grantRequester(t *testing.T, node *delegateNode) *Module {
	t.Helper()

	mod := serveAppsRegistrar(t)
	mod.node = node
	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: grantRequestOp}
	if err := mod.policy.GrantRequest.Set(astral.NewContext(nil), d); err != nil {
		t.Fatalf("set delegate: %v", err)
	}
	return mod
}

// answer waits for the op to close w and returns the text of the
// error_message it answered, or the type of any other object.
func answer(t *testing.T, w *recordingWriter) string {
	t.Helper()
	awaitServeAppsClose(t, w)

	w.mu.Lock()
	defer w.mu.Unlock()
	obj, err := channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
	if err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if msg, ok := obj.(*astral.ErrorMessage); ok {
		return msg.Error()
	}
	return obj.ObjectType()
}

// requestGrant routes q to apphost.request_grant and returns its answer.
func requestGrant(t *testing.T, mod *Module, q *astral.InFlightQuery) string {
	t.Helper()

	w := newRecordingWriter()
	if err := routeQuery(t, mod.OpRequestGrant, q, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	return answer(t, w)
}

// askedOp returns the operation node was asked.
//
// note: the op answers only after the delegate answers, so a delegate that was
// asked has recorded it by the time the answer arrives.
func askedOp(t *testing.T, node *delegateNode) string {
	t.Helper()

	select {
	case op := <-node.queried:
		return op
	default:
		t.Fatal("the delegate was not asked")
		return ""
	}
}

// heldActions returns the actions this node grants id.
func heldActions(t *testing.T, mod *Module, id *astral.Identity) []string {
	t.Helper()

	permits, err := mod.Grants(id)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	var actions []string
	for _, p := range permits {
		actions = append(actions, string(p.Action))
	}
	return actions
}

// The register delegate does not decide grant requests: with only it set, the
// bundled policy refuses and nobody is asked.
func TestRequestGrantRefusedWithoutItsDelegate(t *testing.T) {
	node := newDelegateNode(&apphost.AppRegisterDecision{Allow: true})
	mod := serveAppsRegistrar(t)
	mod.node = node
	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := mod.policy.AppRegister.Set(astral.NewContext(nil), d); err != nil {
		t.Fatalf("set delegate: %v", err)
	}
	owner := astral.GenerateIdentity()

	got := requestGrant(t, mod, sessionQuery(mod, owner, owner, "apphost.request_grant?permits=mod.auth.see_objects_action"))
	if want := apphost.ErrGrantDeclined.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if len(node.queried) != 0 {
		t.Fatal("the register delegate was asked a grant request")
	}
	if held := heldActions(t, mod, owner); len(held) != 0 {
		t.Fatalf("a refused request granted %q", held)
	}
}

// The delegate is asked with the session's identity, its web origin and every
// permit, and its approval grants exactly those permits, unconstrained and
// until revoked.
func TestRequestGrantAsksTheDelegateAndGrants(t *testing.T) {
	node := newDelegateNode(&apphost.GrantDecision{Allow: true})
	mod := grantRequester(t, node)
	owner := astral.GenerateIdentity()
	q := sessionQuery(mod, owner, owner, "apphost.request_grant?permits=mod.auth.see_objects_action,mod.auth.store_objects_action")
	q.Extra.Set(apphostmod.ExtraOriginWeb, "https://music.example")

	if got := requestGrant(t, mod, q); got != ackAnswer {
		t.Fatalf("answered %q; want ack", got)
	}
	if op := askedOp(t, node); op != grantRequestOp {
		t.Fatalf("asked %q, want the configured operation", op)
	}

	req, _ := (<-node.received).(*apphost.GrantRequest)
	if req == nil || !req.Requester.IsEqual(owner) || req.Origin != "https://music.example" {
		t.Fatalf("request reached the delegate as %+v", req)
	}
	var asked []string
	for _, p := range req.Permits {
		asked = append(asked, string(p.Action))
	}
	want := []string{"mod.auth.see_objects_action", "mod.auth.store_objects_action"}
	if !slices.Equal(asked, want) {
		t.Fatalf("delegate was asked %q; want %q", asked, want)
	}

	permits, err := mod.Grants(owner)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(permits) != 2 {
		t.Fatalf("granted %d permits; want 2", len(permits))
	}
	for _, p := range permits {
		if p.Constraints != nil || p.Delegation != 0 {
			t.Fatalf("granted %+v; want an unconstrained, non-delegable permit", p)
		}
	}
	rows, err := mod.db.ListGrants(owner)
	if err != nil {
		t.Fatalf("list rows: %v", err)
	}
	for _, row := range rows {
		if row.ExpiresAt != nil {
			t.Fatalf("grant %v expires at %v; want until revoked", row.Name, row.ExpiresAt)
		}
	}
}

// The delegate's refusal is answered as declined and grants nothing.
func TestRequestGrantRefusedByTheDelegate(t *testing.T) {
	node := newDelegateNode(&apphost.GrantDecision{Allow: false})
	mod := grantRequester(t, node)
	owner := astral.GenerateIdentity()

	got := requestGrant(t, mod, sessionQuery(mod, owner, owner, "apphost.request_grant?permits=mod.auth.see_objects_action"))
	if want := apphost.ErrGrantDeclined.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if op := askedOp(t, node); op != grantRequestOp {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	if held := heldActions(t, mod, owner); len(held) != 0 {
		t.Fatalf("a refused request granted %q", held)
	}
}

// A request from no session, with no permits, or off a link is refused before
// the delegate is asked. A sudo caller's request grants the session's identity.
func TestRequestGrantRefusesBeforeAsking(t *testing.T) {
	owner := astral.GenerateIdentity()
	sudoed := astral.GenerateIdentity()

	for _, tc := range []struct {
		name      string
		principal *astral.Identity
		caller    *astral.Identity
		permits   string
		network   bool
		want      string
	}{
		{"no session", nil, owner, "mod.auth.see_objects_action", false, auth.ErrNotPermitted.Error()},
		{"no permits", owner, owner, ",%20,", false, "missing permits"},
		{"network origin", owner, owner, "mod.auth.see_objects_action", true, astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error()},
		{"sudo caller", owner, sudoed, "mod.auth.see_objects_action", false, ackAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := newDelegateNode(&apphost.GrantDecision{Allow: true})
			mod := grantRequester(t, node)
			q := sessionQuery(mod, tc.principal, tc.caller, "apphost.request_grant?permits="+tc.permits)
			if tc.network {
				q.Extra.Set("origin", astral.OriginNetwork)
			}

			if got := requestGrant(t, mod, q); got != tc.want {
				t.Fatalf("answered %q; want %q", got, tc.want)
			}
			if tc.want != ackAnswer {
				if len(node.queried) != 0 {
					t.Fatal("a refused request reached the delegate")
				}
				if held := heldActions(t, mod, owner); len(held) != 0 {
					t.Fatalf("a refused request granted %q", held)
				}
				return
			}
			if held := heldActions(t, mod, owner); !slices.Equal(held, []string{"mod.auth.see_objects_action"}) {
				t.Fatalf("the session identity holds %q; want the asked permit", held)
			}
			if held := heldActions(t, mod, sudoed); len(held) != 0 {
				t.Fatalf("the sudo caller was granted %q", held)
			}
		})
	}
}

// apphost.set_grant_request_policy requires AdminManageApps, writes the grant
// request delegate alone, and clears it on empty arguments.
func TestSetGrantRequestPolicyWritesItsOwnValue(t *testing.T) {
	id := astral.GenerateIdentity()
	set := func(mod *Module, args string) string {
		w := newRecordingWriter()
		if err := route(t, mod.OpSetGrantRequestPolicy, astral.GenerateIdentity(), "apphost.set_grant_request_policy"+args, w); err != nil {
			t.Fatalf("route: %v", err)
		}
		return answer(t, w)
	}
	delegate := "?identity=" + id.String() + "&operation=" + grantRequestOp

	refused := policySetter(false)
	if got, want := set(refused, delegate), auth.ErrNotPermitted.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if !refused.policy.GrantRequest.Get().IsZero() {
		t.Fatal("a rejected setter wrote a delegate")
	}

	mod := policySetter(true)
	if got, want := set(mod, delegate), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if d := mod.policy.GrantRequest.Get(); d.IsZero() || !d.Identity.IsEqual(id) || d.Operation != grantRequestOp {
		t.Fatalf("persisted %+v, want the delegate as set", d)
	}
	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatal("the grant request setter wrote the register delegate")
	}

	if got, want := set(mod, ""), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if !mod.policy.GrantRequest.Get().IsZero() {
		t.Fatal("an empty set did not clear the delegate")
	}
}
