package apphost

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// pushRecorder is a user module that hands every object pushed to the linked siblings to pushed.
// note: the embedded nil interface panics on any other method, which asserts that registration calls nothing else.
type pushRecorder struct {
	usermod.Module
	pushed chan astral.Object
}

func newPushRecorder() *pushRecorder {
	return &pushRecorder{pushed: make(chan astral.Object, 4)}
}

func (r *pushRecorder) PushToSiblings(_ *astral.Context, obj astral.Object) {
	r.pushed <- obj
}

// drain returns every object pushed so far.
// note: registration pushes before it closes the caller's connection, so after
// awaitServeAppsClose every push has already arrived.
func (r *pushRecorder) drain() (objs []astral.Object) {
	for {
		select {
		case obj := <-r.pushed:
			objs = append(objs, obj)
		default:
			return objs
		}
	}
}

// TestRegistrationPushesTheRelayContractToTheSiblings: apphost.register pushes
// the new app's relay-for contract, issued by the app to this node, to the
// linked siblings, so a sibling linked before the registration can route to the app.
func TestRegistrationPushesTheRelayContractToTheSiblings(t *testing.T) {
	mod := serveAppsRegistrar(t)
	recorder := newPushRecorder()
	mod.User = recorder
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	tokens := issuedTokens(t, mod)
	if len(tokens) != 1 {
		t.Fatalf("apphost.register issued %d tokens; want 1", len(tokens))
	}
	app := tokens[0].Identity

	pushed := recorder.drain()
	if len(pushed) != 1 {
		t.Fatalf("apphost.register pushed %d objects to the siblings; want the relay contract alone", len(pushed))
	}

	signed, ok := pushed[0].(*auth.SignedContract)
	if !ok {
		t.Fatalf("apphost.register pushed %v; want a signed contract", pushed[0].ObjectType())
	}
	if !signed.Issuer.IsEqual(app) {
		t.Fatalf("the pushed contract is issued by %v; want the app %v", signed.Issuer, app)
	}
	if !signed.Subject.IsEqual(mod.node.Identity()) {
		t.Fatalf("the pushed contract names subject %v; want this node %v", signed.Subject, mod.node.Identity())
	}
	if len(signed.Permits) != 1 || string(signed.Permits[0].Action) != (nodes.RelayForAction{}).ObjectType() {
		t.Fatalf("the pushed contract carries %d permits; want one %v", len(signed.Permits), (nodes.RelayForAction{}).ObjectType())
	}
}

// TestRegistrationThatFailsPushesNothing: a registration that fails after its
// relay contract is indexed issues no token, and the siblings receive no
// contract of an app nobody holds a token for.
func TestRegistrationThatFailsPushesNothing(t *testing.T) {
	steps := &registerSteps{setErr: errors.New("endpoint names no transport")}
	mod := delegatedRegistrar(t, steps, newDelegateNode(&apphost.AppRegisterDecision{
		Allow:       true,
		Authorizers: []*auth.AuthorizerAssignment{{Action: "mod.auth.see_objects_action", Endpoint: "ftp://nowhere"}},
	}))
	recorder := newPushRecorder()
	mod.User = recorder
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	if tokens := issuedTokens(t, mod); len(tokens) != 0 {
		t.Fatalf("a failed assignment issued %d tokens", len(tokens))
	}
	if pushed := recorder.drain(); len(pushed) != 0 {
		t.Fatalf("a failed registration pushed %d objects to the siblings; want none", len(pushed))
	}
}
