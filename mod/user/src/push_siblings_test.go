package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// siblingPush is one push the user module made.
type siblingPush struct {
	target   *astral.Identity
	deadline time.Time
}

// deadlinePushRecorder is an objects module that records the target and deadline of every push.
type deadlinePushRecorder struct {
	objectsmod.Module
	pushed chan siblingPush
}

func (r *deadlinePushRecorder) Push(ctx *astral.Context, target *astral.Identity, _ astral.Object) error {
	deadline, _ := ctx.Deadline()
	r.pushed <- siblingPush{target: target, deadline: deadline}
	return nil
}

// TestPushToSiblingsReachesTheLinkedSiblingsAlone: a push goes to each linked
// sibling under a deadline, and never to a member without a link.
func TestPushToSiblingsReachesTheLinkedSiblingsAlone(t *testing.T) {
	linked, unlinked := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := siblingFixture(t, []*astral.Identity{unlinked, linked}, []*astral.Identity{linked})
	recorder := &deadlinePushRecorder{pushed: make(chan siblingPush, 4)}
	mod.Deps.Objects = recorder

	mod.PushToSiblings(astral.NewContext(nil), &astral.EOS{})

	select {
	case push := <-recorder.pushed:
		if !push.target.IsEqual(linked) {
			t.Fatalf("pushed to %v; want the linked sibling %v", push.target, linked)
		}
		if push.deadline.IsZero() {
			t.Fatal("the push has no deadline")
		}
	case <-time.After(siblingTestTimeout):
		t.Fatal("nothing was pushed to the linked sibling")
	}

	select {
	case push := <-recorder.pushed:
		t.Fatalf("pushed a second time, to %v; want the linked sibling alone", push.target)
	case <-time.After(100 * time.Millisecond):
	}
}
