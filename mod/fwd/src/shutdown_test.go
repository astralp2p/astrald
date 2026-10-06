package fwd

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// blockingServer runs until its context is cancelled.
type blockingServer struct{}

func (blockingServer) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (blockingServer) String() string { return "blocking" }

func (blockingServer) Target() astral.Router { return nil }

func TestServerRunnerDoneBeforeRun(t *testing.T) {
	srv := NewServerRunner(astral.NewContext(context.Background()), blockingServer{})

	if srv.Done() == nil {
		t.Fatal("Done() returned a nil channel before Run")
	}
}

func TestModuleRunReturnsWhenCancelledRightAfterStart(t *testing.T) {
	for i := 0; i < 200; i++ {
		mod := &Module{servers: make(map[*ServerRunner]struct{})}
		ctx, cancel := astral.NewContext(context.Background()).WithCancel()

		if err := mod.runServer(NewServerRunner(ctx, blockingServer{})); err != nil {
			t.Fatalf("runServer: %v", err)
		}
		cancel()

		returned := make(chan struct{})
		go func() {
			defer close(returned)
			mod.Run(ctx)
		}()

		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Module.Run did not return after cancel", i)
		}
	}
}
