package messaging

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// startWait routes one wait for the caller and answers the caller's side of the
// connection once the op has accepted, with the writer the op answers on.
func startWait(t *testing.T, mod *Module, caller *astral.Identity, args string) (conn interface{ Close() error }, w *collectingWriter) {
	t.Helper()

	op, err := routing.NewOp(mod.OpWait)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	w = newCollectingWriter()
	start := time.Now()

	conn, err = op.RouteQuery(ctx, originQuery(caller, "messaging.wait"+args, astral.OriginLocal), w)
	if err != nil {
		t.Fatalf("wait refused: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the wait was accepted after %v; it must accept before it parks", took)
	}
	return conn, w
}

// A park whose caller closes the channel ends: nobody is left to answer, and a
// park that outlived its caller would hold its registration for the whole
// window.
func TestAWaitEndsWhenTheCallerCloses(t *testing.T) {
	mod := testMessagingModule(t)
	owner := hostedParticipant(t, mod)

	conn, w := startWait(t, mod, owner, "?timeout=1m")
	waitUntilParked(t, mod, owner, 1)

	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the park outlived its caller")
	}

	deadline := time.Now().Add(3 * time.Second)
	for mod.waiters.parked(owner) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the park left its registration behind")
		}
		time.Sleep(time.Millisecond)
	}
}

// A delivery while the wait is parked answers it with the envelope, without the
// body, and the cursor past it.
func TestAWaitAnswersWhatArrives(t *testing.T) {
	mod := testMessagingModule(t)
	owner, sender := hostedParticipant(t, mod), astral.GenerateIdentity()

	conn, w := startWait(t, mod, owner, "?timeout=1m")
	defer conn.Close()
	waitUntilParked(t, mod, owner, 1)

	id := messaging.NewMessageID()
	if err := mod.storeMessage(sender, owner, &messaging.Message{ID: id, Content: "x"}); err != nil {
		t.Fatalf("store: %v", err)
	}

	objs := w.objects(t)
	if len(objs) != 1 {
		t.Fatalf("the wait answered %v objects, want one result", len(objs))
	}
	res, ok := objs[0].(*messaging.WaitResult)
	if !ok {
		t.Fatalf("the wait answered %T, want *messaging.WaitResult", objs[0])
	}
	if res.TimedOut || len(res.Messages) != 1 || res.Messages[0].ID != id {
		t.Fatalf("the wait answered %+v, want the delivered message", res)
	}
	if uint64(res.NextSince) != uint64(res.Messages[0].Cursor) {
		t.Fatalf("next_since %v, want the delivered row's cursor %v", res.NextSince, res.Messages[0].Cursor)
	}
}
