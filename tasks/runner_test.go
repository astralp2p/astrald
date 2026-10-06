package tasks

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

func TestRunWithoutRunners(t *testing.T) {
	if err := Run(astral.NewContext(nil)); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

func TestRunSkipsNilRunner(t *testing.T) {
	var calls atomic.Uint64
	f := func(*astral.Context) error {
		calls.Add(1)
		return nil
	}

	if err := Run(astral.NewContext(nil), nil, f); err != nil {
		t.Fatalf("Run(nil, f) = %v, want nil", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("f called %d times, want 1", got)
	}
}

func TestRunJoinsErrors(t *testing.T) {
	e1, e2 := errors.New("e1"), errors.New("e2")

	err := Run(astral.NewContext(nil),
		func(*astral.Context) error { return e1 },
		func(*astral.Context) error { return e2 },
	)

	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("Run error = %v, want wrapping %v and %v", err, e1, e2)
	}
}

func TestRunRunsConcurrently(t *testing.T) {
	c1, c2 := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- Run(astral.NewContext(nil),
			func(*astral.Context) error {
				close(c1)
				<-c2
				return nil
			},
			func(*astral.Context) error {
				close(c2)
				<-c1
				return nil
			},
		)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return: runners are not started concurrently")
	}
}

func TestGroupRun(t *testing.T) {
	e1, e2 := errors.New("e1"), errors.New("e2")
	var calls1, calls2 atomic.Uint64

	err := Group(
		Func(func(*astral.Context) error {
			calls1.Add(1)
			return e1
		}),
		Func(func(*astral.Context) error {
			calls2.Add(1)
			return e2
		}),
	).Run(astral.NewContext(nil))

	if calls1.Load() != 1 || calls2.Load() != 1 {
		t.Fatalf("runner calls = (%d, %d), want (1, 1)", calls1.Load(), calls2.Load())
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("Group.Run error = %v, want wrapping %v and %v", err, e1, e2)
	}
}

func TestFuncRunnerNilPanics(t *testing.T) {
	defer func() {
		if p := recover(); p != "func is nil" {
			t.Fatalf("recovered %v, want panic %q", p, "func is nil")
		}
	}()

	FuncRunner{}.Run(astral.NewContext(nil))
}
