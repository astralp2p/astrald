package apphost

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

const grantRequestOp = "control.decide_grant_request"

// ackAnswer is what readReply returns for an ack.
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

// readReply waits for the op to close w and returns the text of the
// error_message it answered, or the type of any other object.
func readReply(t *testing.T, w *recordingWriter) string {
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

// requestGrant routes q to apphost.request_grant, sends req as its input, and
// returns its answer.
func requestGrant(t *testing.T, mod *Module, q *astral.InFlightQuery, req *apphost.RequestGrantRequest) string {
	t.Helper()

	w := newRecordingWriter()
	if err := routeQuerySending(t, mod.OpRequestGrant, q, req, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	return readReply(t, w)
}

// asking returns a request_grant_request for unconstrained permits of actions.
func asking(actions ...string) *apphost.RequestGrantRequest {
	return &apphost.RequestGrantRequest{Permits: permits(actions...)}
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
	return actions(permits)
}

// The register delegate does not decide grant requests: with only it set, the
// bundled policy grants and nobody is asked.
func TestRequestGrantIgnoresTheRegisterDelegate(t *testing.T) {
	node := newDelegateNode(&apphost.AppRegisterDecision{Allow: true})
	mod := serveAppsRegistrar(t)
	mod.node = node
	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := mod.policy.AppRegister.Set(astral.NewContext(nil), d); err != nil {
		t.Fatalf("set delegate: %v", err)
	}
	owner := astral.GenerateIdentity()

	got := requestGrant(t, mod, sessionQuery(mod, owner, owner, "apphost.request_grant"), asking("mod.auth.see_objects_action"))
	if got != ackAnswer {
		t.Fatalf("answered %q; want %q", got, ackAnswer)
	}
	if len(node.queried) != 0 {
		t.Fatal("the register delegate was asked a grant request")
	}
	if held := heldActions(t, mod, owner); len(held) != 1 {
		t.Fatalf("the identity holds %q; want the one permit", held)
	}
}

// The delegate is asked with the session's identity, its web origin and every
// permit as sent, and its approval grants exactly those permits, non-delegable
// and until revoked.
func TestRequestGrantAsksTheDelegateAndGrants(t *testing.T) {
	node := newDelegateNode(&apphost.GrantDecision{Allow: true})
	mod := grantRequester(t, node)
	owner := astral.GenerateIdentity()
	q := sessionQuery(mod, owner, owner, "apphost.request_grant")
	q.Extra.Set(apphostmod.ExtraOriginWeb, "https://music.example")
	req := asking("mod.auth.see_objects_action", "mod.auth.store_objects_action")
	req.Permits[1].Delegation = 2

	if got := requestGrant(t, mod, q, req); got != ackAnswer {
		t.Fatalf("answered %q; want ack", got)
	}
	if op := askedOp(t, node); op != grantRequestOp {
		t.Fatalf("asked %q, want the configured operation", op)
	}

	policyReq, _ := (<-node.received).(*apphost.GrantPolicyRequest)
	if policyReq == nil || !policyReq.Requester.IsEqual(owner) || policyReq.Origin != "https://music.example" || policyReq.Duration != 0 {
		t.Fatalf("request reached the delegate as %+v", policyReq)
	}
	asked := actions(policyReq.Permits)
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

// An approved request adds its permits beside the grants the identity already
// holds, so an earlier scoped discovery grant keeps authorizing.
func TestApprovedRequestKeepsAnEarlierScopedGrant(t *testing.T) {
	node := newDelegateNode(&apphost.GrantDecision{Allow: true})
	mod := grantRequester(t, node)
	owner, target := astral.GenerateIdentity(), astral.GenerateIdentity()
	action := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())

	if err := mod.Grant(owner, &auth.Permit{Action: action, Constraints: playerScope()}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	q := sessionQuery(mod, owner, owner, "apphost.request_grant")
	req := &apphost.RequestGrantRequest{Permits: []*auth.Permit{discoveryPermit("wallet")}}
	if got := requestGrant(t, mod, q, req); got != ackAnswer {
		t.Fatalf("answered %q; want ack", got)
	}

	granted, err := mod.Grants(owner)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(granted) != 2 || string(granted[0].Action) != string(action) || string(granted[1].Action) != string(action) {
		t.Fatalf("granted %+v; want the earlier scoped permit, then the requested one", granted)
	}
	for _, service := range []astral.String8{"player", "wallet"} {
		if !mod.AuthorizeServiceDiscovery(nil, discoveryAction(owner, target, service)) {
			t.Fatalf("the grants must cover %v", service)
		}
	}
}

// The delegate's refusal is answered as declined and grants nothing.
func TestRequestGrantRefusedByTheDelegate(t *testing.T) {
	node := newDelegateNode(&apphost.GrantDecision{Allow: false})
	mod := grantRequester(t, node)
	owner := astral.GenerateIdentity()

	got := requestGrant(t, mod, sessionQuery(mod, owner, owner, "apphost.request_grant"), asking("mod.auth.see_objects_action"))
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

// A request from no session, off a link, naming a removed argument, with no
// permits, with a negative duration, or with an invalid permit is refused
// before the delegate is asked. A sudo caller's request grants the session's
// identity.
func TestRequestGrantRefusesBeforeAsking(t *testing.T) {
	owner := astral.GenerateIdentity()
	sudoed := astral.GenerateIdentity()
	seeObjects := asking("mod.auth.see_objects_action")
	discovery := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())
	twoScopes := discoveryPermit("inference")
	if err := twoScopes.Constraints.Append(discoveryPermit("chat").Constraints.Objects()[0]); err != nil {
		t.Fatalf("scope: %v", err)
	}
	invalidDiscovery := "invalid permit: " + string(discovery) + ": "

	for _, tc := range []struct {
		name      string
		principal *astral.Identity
		caller    *astral.Identity
		query     string
		req       *apphost.RequestGrantRequest
		network   bool
		want      string
	}{
		{"no session", nil, owner, "", seeObjects, false, auth.ErrNotPermitted.Error()},
		{"network origin", owner, owner, "", seeObjects, true, astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error()},
		{"removed permits argument", owner, owner, "?permits=mod.auth.see_objects_action", seeObjects, false, "unknown argument: permits"},
		{"no permits", owner, owner, "", &apphost.RequestGrantRequest{}, false, "missing permits"},
		{"negative duration", owner, owner, "", &apphost.RequestGrantRequest{Permits: seeObjects.Permits, Duration: -1}, false, "negative duration"},
		{"unscoped discovery", owner, owner, "", asking(string(discovery)), false, invalidDiscovery + "requires constraints"},
		{"empty bundle", owner, owner, "", &apphost.RequestGrantRequest{Permits: []*auth.Permit{{Action: discovery, Constraints: astral.NewBundle()}}}, false, invalidDiscovery + "requires constraints"},
		{"two scopes", owner, owner, "", &apphost.RequestGrantRequest{Permits: []*auth.Permit{twoScopes}}, false, ""},
		{"sudo caller", owner, sudoed, "", seeObjects, false, ackAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := newDelegateNode(&apphost.GrantDecision{Allow: true})
			mod := grantRequester(t, node)
			q := sessionQuery(mod, tc.principal, tc.caller, "apphost.request_grant"+tc.query)
			if tc.network {
				q.Extra.Set("origin", astral.OriginNetwork)
			}

			got := requestGrant(t, mod, q, tc.req)
			if tc.want == "" {
				if !strings.HasPrefix(got, invalidDiscovery) {
					t.Fatalf("answered %q; want %q…", got, invalidDiscovery)
				}
			} else if got != tc.want {
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
		return readReply(t, w)
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

	if got, want := set(mod, "?mode=accept_all"), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if m, ok := mod.policy.GrantRequestMode.Get().(*astral.String8); !ok || m.String() != apphostmod.GrantRequestAcceptAll {
		t.Fatalf("persisted mode %v; want accept_all", m)
	}
	if !mod.policy.GrantRequest.Get().IsZero() {
		t.Fatal("a mode did not clear the delegate")
	}

	if got, want := set(mod, delegate), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if m := mod.policy.GrantRequestMode.Get(); m != nil {
		t.Fatalf("a delegate left the mode %v", m)
	}

	if got, want := set(mod, "?mode=accept_all&identity="+id.String()), "give mode or a delegate, not both"; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if got, want := set(mod, "?mode=bogus"), "unknown mode: bogus"; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if got, want := set(mod, "?mode=accept_all&timeout=1m"), "unknown argument: timeout"; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if d := mod.policy.GrantRequest.Get(); d.IsZero() || mod.policy.GrantRequestMode.Get() != nil {
		t.Fatal("a refused set changed the delegate or the mode")
	}

	if got, want := set(mod, "?mode=reject_all"), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if got, want := set(mod, ""), ackAnswer; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if !mod.policy.GrantRequest.Get().IsZero() || mod.policy.GrantRequestMode.Get() != nil {
		t.Fatal("an empty set did not clear the delegate and the mode")
	}
}
