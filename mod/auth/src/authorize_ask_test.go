package auth

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// waitingAsk answers nothing until its context ends, and records the deadline it was given.
type waitingAsk struct {
	deadline time.Time
}

func (w *waitingAsk) Ask(ctx *astral.Context, _ auth.ActionObject) (bool, error) {
	w.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return false, ctx.Err()
}

// TestNewAuthorizeAskSelectsTheTransport: the exported constructor builds the asker an external authorizer
// builds, and refuses an endpoint naming no transport.
func TestNewAuthorizeAskSelectsTheTransport(t *testing.T) {
	mod := testModule(t)

	for endpoint, want := range map[string]string{
		"astral://telepathy:vfs.authorize": "*auth.astralAuthorizer",
		"https://authz.example/authorize":  "*auth.httpAuthorizer",
	} {
		ask, err := mod.NewAuthorizeAsk(endpoint, 0)
		if err != nil {
			t.Fatalf("%v: %v", endpoint, err)
		}
		bounded, ok := ask.(*boundedAsk)
		if !ok {
			t.Fatalf("%v: got %T, want a bounded asker", endpoint, ask)
		}
		if got := fmt.Sprintf("%T", bounded.ask); got != want {
			t.Fatalf("%v: transport %v, want %v", endpoint, got, want)
		}
	}

	for _, endpoint := range []string{"", "telepathy:vfs.authorize", "astral://telepathy", "grpc://authz.example"} {
		if _, err := mod.NewAuthorizeAsk(endpoint, 0); err == nil {
			t.Errorf("%q was accepted", endpoint)
		}
	}
}

// TestNewAuthorizeAskBoundsEveryQuestion: a question that gets no answer ends at the timeout, and a timeout of 0
// is the default of an external authorizer.
func TestNewAuthorizeAskBoundsEveryQuestion(t *testing.T) {
	mod := testModule(t)

	for timeout, want := range map[time.Duration]time.Duration{
		0:                     defaultExternal.Timeout,
		50 * time.Millisecond: 50 * time.Millisecond,
	} {
		ask, err := mod.NewAuthorizeAsk("astral://telepathy:vfs.authorize", timeout)
		if err != nil {
			t.Fatal(err)
		}
		waiting := &waitingAsk{}
		ask.(*boundedAsk).ask = waiting

		start := time.Now()
		allow, err := ask.Ask(astral.NewContext(nil), action(astral.GenerateIdentity()))
		if allow || err == nil {
			t.Fatalf("timeout %v: an unanswered question answered %v, %v", timeout, allow, err)
		}
		if bound := waiting.deadline.Sub(start); bound <= 0 || bound > want+50*time.Millisecond {
			t.Fatalf("timeout %v: the question was bounded by %v, want %v", timeout, bound, want)
		}
	}
}

// silentNode accepts every query and never answers it.
type silentNode struct{ id *astral.Identity }

func (n silentNode) Identity() *astral.Identity { return n.id }

func (silentNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return discard{}, nil
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }

// readlessNode accepts every query and never reads the question.
type readlessNode struct{ id *astral.Identity }

func (n readlessNode) Identity() *astral.Identity { return n.id }

// note: io.Pipe has no buffer, so a write into a pipe nobody reads blocks until the pipe is closed.
func (readlessNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	_, w := io.Pipe()
	return w, nil
}

// askerGoroutines counts the goroutines running inside the astral asker.
func askerGoroutines() int {
	buf := make([]byte, 1<<22)
	stacks := string(buf[:runtime.Stack(buf, true)])

	return strings.Count(stacks, "astralAuthorizer).Ask") + strings.Count(stacks, "auth.closeWhenDone")
}

// askWithin asks through the astral asker on node, with a timeout of 50ms, and fails the test unless the
// question is refused within 2s and no goroutine of the asker remains.
func askWithin(t *testing.T, node astral.Node) {
	t.Helper()

	mod := testModule(t)
	mod.node = node
	ask, err := mod.NewAuthorizeAsk("astral://telepathy:vfs.authorize", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	authority := astral.GenerateIdentity()
	ask.(*boundedAsk).ask.(*astralAuthorizer).resolve = func(string) (*astral.Identity, error) { return authority, nil }

	done := make(chan error, 1)
	go func() {
		allow, err := ask.Ask(astral.NewContext(nil), action(astral.GenerateIdentity()))
		if allow {
			err = fmt.Errorf("allowed")
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an unanswered question answered without an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an unanswered question outlived its timeout")
	}
	if n := askerGoroutines(); n != 0 {
		t.Fatalf("%d goroutines of the asker remain after Ask returned", n)
	}
}

// TestAstralAskerEndsAnUnansweredQuestionAtTheTimeout: an authority that reads the question and never answers
// refuses when the timeout ends, not later.
func TestAstralAskerEndsAnUnansweredQuestionAtTheTimeout(t *testing.T) {
	askWithin(t, silentNode{id: astral.GenerateIdentity()})
}

// TestAstralAskerEndsAnUnreadQuestionAtTheTimeout: an authority that accepts the query and never reads the
// question refuses when the timeout ends, not later.
func TestAstralAskerEndsAnUnreadQuestionAtTheTimeout(t *testing.T) {
	askWithin(t, readlessNode{id: astral.GenerateIdentity()})
}
