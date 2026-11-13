package vfs_test

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostapiclient "github.com/astralp2p/astral-go/api/apphost/client"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	libapphost "github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/query"
)

// TestAcceptanceEvaluatorCannotNameAnotherOp: an app that names an op with arguments as its evaluator, so that the
// node would run it as itself, is refused when it sets the field, and no other VFS changes.
func TestAcceptanceEvaluatorCannotNameAnotherOp(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	attacker := a.registerApp()
	helper := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	victim := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	own := a.evaluatorOf(attacker)

	inner := "astral://" + attacker.id.String() + ":vfs.authorize"
	for _, endpoint := range []string{
		"astral://localnode:vfs.set_evaluator?vfs=" + victim.id.String() + "&endpoint=" + url.QueryEscape(inner),
		"astral://localnode:vfs.set_evaluator",
		"astral://localnode:vfs.authorize?vfs=" + victim.id.String(),
		"astral://localnode@" + attacker.id.String() + ":vfs.authorize",
	} {
		answer, err := a.call(attacker.client, "vfs.set_evaluator", query.Args{"vfs": attacker.id, "endpoint": endpoint})
		if e, isErr := answer.(astral.Error); err != nil || !isErr || e.Error() != vfsapi.ErrInvalidEndpoint.Error() {
			t.Fatalf("vfs.set_evaluator %.60q answered %#v, %v; want invalid endpoint", endpoint, answer, err)
		}
	}
	if got := a.evaluatorOf(attacker); got != own {
		t.Fatalf("a refused endpoint changed the attacker's evaluator to %q", got)
	}

	// note: the helper's read makes the node ask the attacker's evaluator, which is the attacker and not running.
	if a.mayRead(helper, attacker) {
		t.Fatal("the helper read the attacker's VFS")
	}
	if got := a.evaluatorOf(victim); got != "" {
		t.Fatalf("the victim's evaluator is now %q", got)
	}
}

// TestAcceptanceEvaluatorThatNeverReadsIsRefusedInTime: an evaluator that accepts the question and never reads
// it refuses the caller within about the auth module's default timeout, and no goroutine of the asker remains.
func TestAcceptanceEvaluatorThatNeverReadsIsRefusedInTime(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner := a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	e, _ := a.serveEvaluator(owner)
	e.answer.Store(int32(answerSilent))

	start := time.Now()
	if a.mayRead(caller, owner) {
		t.Fatal("an evaluator that never reads let the caller in")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the refusal took %v", took)
	}

	awaitNoAskerGoroutines(t)
}

// TestAcceptanceEvaluatorThatNeverAcksIsRefusedInTime: an evaluator whose endpoint reads the query and never acks
// or rejects it refuses the caller within about the auth module's default timeout, and no goroutine of the asker
// or of the routing to the app remains.
func TestAcceptanceEvaluatorThatNeverAcksIsRefusedInTime(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner := a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	held := a.serveSilentEndpoint(owner)

	start := time.Now()
	if a.mayRead(caller, owner) {
		t.Fatal("an evaluator that never acks let the caller in")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the refusal took %v", took)
	}
	if held.Load() == 0 {
		t.Fatal("the evaluator was not asked")
	}

	awaitNoAskerGoroutines(t)
}

// TestAcceptanceOwnerReadsItsObjectPastAnotherEvaluator: an app reads its own object by ObjectID through
// objects.read while another VFS holds the same content and names an evaluator that never acks; the read is
// allowed without asking that evaluator, whichever VFS the database lists first.
func TestAcceptanceOwnerReadsItsObjectPastAnotherEvaluator(t *testing.T) {
	for _, attackerFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("attackerFirst=%v", attackerFirst), func(t *testing.T) {
			a := startAcceptanceNode(t)
			a.claim()
			attacker := a.registerApp()
			victim := a.registerApp()
			content := []byte("content two VFSes hold")

			if attackerFirst {
				a.mustWrite(attacker.client, "/x", content)
			}
			entry := a.mustWrite(victim.client, "/x", content)
			if !attackerFirst {
				a.mustWrite(attacker.client, "/x", content)
			}
			held := a.serveSilentEndpoint(attacker)

			start := time.Now()
			got, err := a.readRaw(victim.client, "objects.read", query.Args{"id": entry.ID.String()})
			if err != nil || string(got) != string(content) {
				t.Fatalf("objects.read of the victim's own object: %q, %v", got, err)
			}
			if took := time.Since(start); took > 2*time.Second {
				t.Fatalf("the read took %v", took)
			}
			if n := held.Load(); n != 0 {
				t.Fatalf("the attacker's evaluator was asked %d times", n)
			}
		})
	}
}

// serveSilentEndpoint registers an IPC endpoint for app that reads every query and never acks or rejects it. It
// returns the number of queries read.
func (a *acceptanceNode) serveSilentEndpoint(app *acceptanceApp) *atomic.Uint64 {
	a.t.Helper()

	h, err := apps.NewHandlerOn(fmt.Sprintf("memu:vfs-silent-%d", acceptanceListeners.Add(1)), astral.NewNonce())
	if err != nil {
		a.t.Fatalf("new handler: %v", err)
	}
	a.t.Cleanup(func() { h.Close() })

	var held atomic.Uint64
	go func() {
		for {
			// note: the node closes its end of a held query when its context ends.
			if _, err := h.ReadQuery(); err != nil {
				return
			}
			held.Add(1)
		}
	}()

	ctx, cancel := a.ctx.WithCancel()
	a.t.Cleanup(cancel)
	reg := apps.NewAppRegistrar(ctx, apps.WithClient(apphostapiclient.New(a.nodeID(), app.client)))

	// why: Ready reports the bind, and Register returns once the node holds the endpoint.
	wait, stop := ctx.WithTimeout(acceptanceWait)
	defer stop()
	if err = reg.Register(wait, h.Endpoint(), h.Token()); err != nil {
		a.t.Fatalf("register the silent endpoint: %v", err)
	}

	return &held
}

// awaitNoAskerGoroutines fails the test when a goroutine of the asker or of IPC routing outlives about a second.
//
// note: the answer reaches the caller after Ask returns; the poll covers the node's own teardown.
func awaitNoAskerGoroutines(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for askerGoroutines() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines of the asker or of IPC routing remain", askerGoroutines())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// askerGoroutines counts the goroutines running inside the auth module's astral asker and inside routing to an
// IPC endpoint or a service handler.
func askerGoroutines() int {
	buf := make([]byte, 1<<22)
	stacks := string(buf[:runtime.Stack(buf, true)])

	return strings.Count(stacks, "astralAuthorizer).") + strings.Count(stacks, "auth.closeWhenDone") +
		strings.Count(stacks, "IPCHandler).RouteQuery") + strings.Count(stacks, "apphost.handshake") +
		strings.Count(stacks, "ServiceHandler).RouteQuery")
}

// TestAcceptanceSilentServiceEvaluatorIsRefusedInTime: the evaluator identity has a service handler
// whose registration connection the app stops reading; a stranger's read is refused within about the timeout.
func TestAcceptanceSilentServiceEvaluatorIsRefusedInTime(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner := a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	registerSilentService(t, a, owner)

	start := time.Now()
	done := make(chan bool, 1)
	go func() { done <- a.mayRead(caller, owner) }()

	select {
	case allowed := <-done:
		if allowed {
			t.Fatal("a silent evaluator allowed the read")
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("refused after %v, want about the timeout", d)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("the read is still blocked by a silent service evaluator")
	}
	awaitNoAskerGoroutines(t)
}

// registerSilentService registers a service handler for the app and then never reads its registration connection.
func registerSilentService(t *testing.T, a *acceptanceNode, app *acceptanceApp) {
	t.Helper()

	host, err := libapphost.Connect(astral.NewContext(nil), a.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	if err := host.AuthToken(app.token); err != nil {
		t.Fatal(err)
	}
	if err := host.Send(&apphostapi.RegisterServiceMsg{Identity: app.id}); err != nil {
		t.Fatal(err)
	}
	if reply, err := host.Receive(); err != nil {
		t.Fatal(err)
	} else if _, ok := reply.(*astral.Ack); !ok {
		t.Fatalf("register_service answered %#v", reply)
	}
}
