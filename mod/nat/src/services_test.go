package nat

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestNodePunchRefusesWhileDisabled holds the signal an initiator relies on: it
// holds no permission to discover this node's nat service, so a node that does
// not traverse NAT refuses the punch itself, before accepting anything.
func TestNodePunchRefusesWhileDisabled(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	err := route(t, mod.OpNodePunch, astral.GenerateIdentity(), "nat.node_punch", w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("a disabled node answered a punch: got err %v, want a rejection", err)
	}
	if n := w.written(); n != 0 {
		t.Fatalf("wrote %d bytes; want none", n)
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
