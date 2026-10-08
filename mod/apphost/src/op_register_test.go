package apphost

import (
	"testing"
	"time"

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

func (r *pushRecorder) PushToSiblings(_ *astral.Context, obj astral.Object) {
	r.pushed <- obj
}

// TestRegistrationPushesTheRelayContractToTheSiblings: apphost.register pushes
// the new app's relay-for contract, issued by the app to this node, to the
// linked siblings, so a sibling linked before the registration can route to the app.
func TestRegistrationPushesTheRelayContractToTheSiblings(t *testing.T) {
	mod := serveAppsRegistrar(t)
	recorder := &pushRecorder{pushed: make(chan astral.Object, 4)}
	mod.User = recorder
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("apphost.register issued %d tokens; want 1", len(tokens))
	}
	app := tokens[0].Identity

	var obj astral.Object
	select {
	case obj = <-recorder.pushed:
	case <-time.After(10 * time.Second):
		t.Fatal("apphost.register pushed nothing to the siblings")
	}

	signed, ok := obj.(*auth.SignedContract)
	if !ok {
		t.Fatalf("apphost.register pushed %v; want a signed contract", obj.ObjectType())
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

	select {
	case extra := <-recorder.pushed:
		t.Fatalf("apphost.register pushed a second object %v; want only the relay contract", extra.ObjectType())
	case <-time.After(100 * time.Millisecond):
	}
}
