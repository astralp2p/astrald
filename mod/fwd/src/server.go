package fwd

import (
	"context"
	"fmt"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/tasks"
	"time"
)

// Server is the contract for a forwarding listener: it can run, report its target, and format itself for logging.
type Server interface {
	tasks.Runner
	fmt.Stringer
	Target() astral.Router
}

// ServerRunner wraps a Server with a cancellable context and a done signal.
type ServerRunner struct {
	Server
	startedAt time.Time
	ctx       *astral.Context
	cancel    context.CancelFunc
	err       error
	done      chan struct{}
}

func NewServerRunner(ctx *astral.Context, s Server) *ServerRunner {
	ctx, cancel := ctx.WithCancel()

	return &ServerRunner{
		Server:    s,
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
}

// Run delegates to the wrapped Server and closes done on return. Run must be called at most once.
func (srv *ServerRunner) Run(ctx *astral.Context) error {
	defer close(srv.done)
	srv.ctx = ctx
	srv.err = srv.Server.Run(ctx)
	return srv.err
}

func (srv *ServerRunner) Done() <-chan struct{} {
	return srv.done
}

func (srv *ServerRunner) String() string {
	return srv.Server.String()
}

func (srv *ServerRunner) Stop() error {
	srv.cancel()
	return nil
}
