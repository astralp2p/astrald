package fwd

import (
	"context"
	"fmt"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/tasks"
	"time"
)

// Server is the contract for a forwarding listener: it can run, report its target, and format itself for logging.
type Server interface {
	tasks.Runner
	fmt.Stringer
	Target() astral.Router
}

// ServerRunner wraps a Server with a cancellable context and a done signal.
// note: the done channel is nil until Run is called; callers must not invoke Done before Run.
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
	}
}

// Run initializes the done channel, delegates to the wrapped Server, and closes done on return.
func (srv *ServerRunner) Run(ctx *astral.Context) error {
	srv.done = make(chan struct{})
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
