package nat

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
)

// TestNodePunchRefusesWhileDisabled holds the signal an initiator relies on: it
// holds no permission to discover this node's nat service, so a node that does
// not traverse NAT answers nat.ErrDisabled and nothing else.
func TestNodePunchRefusesWhileDisabled(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	if err := route(t, mod.OpNodePunch, astral.GenerateIdentity(), "nat.node_punch", w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}

	objs := w.objects(t)
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want one error_message", objs)
	}
	if got, want := objs[0].(astral.Error).Error(), nat.ErrDisabled.Error(); got != want {
		t.Fatalf("error %q; want %q", got, want)
	}
}

func TestEvaluateFollowsEnabled(t *testing.T) {
	mod := &Module{}
	if u := mod.Evaluate(nil, "nat"); bool(u.Available) {
		t.Fatal("offered nat while disabled")
	}
	mod.SetEnabled(true)
	if u := mod.Evaluate(nil, "nat"); !bool(u.Available) {
		t.Fatal("did not offer nat while enabled")
	}
}
