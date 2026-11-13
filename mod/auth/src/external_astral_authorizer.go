package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/lib/arl"
)

const astralScheme = "astral://"

var _ auth.AuthorizeAsk = &astralAuthorizer{}

// astralAuthorizer puts the question to an identity, as a query carrying the action.
type astralAuthorizer struct {
	node    astral.Node
	resolve func(string) (*astral.Identity, error)

	// name is the authority as configured — a hex public key or an alias — and
	// path the op it serves.
	name string
	path string

	// target is resolved on first use and held. Resolution reaches the dir
	// module, and LoadDependencies runs every module concurrently, so an asker
	// resolving at load would call a module that may not have loaded its own.
	mu     sync.Mutex
	target *astral.Identity
}

// newAstralAuthorizer parses an astral:// endpoint of the form
// astral://<identity-or-alias>:<query>.
func newAstralAuthorizer(mod *Module, config ExternalConfig) (*astralAuthorizer, error) {
	_, target, path := arl.Split(strings.TrimPrefix(config.Endpoint, astralScheme))

	switch {
	case target == "":
		return nil, fmt.Errorf("%v names no authority", config.Endpoint)
	case path == "":
		return nil, fmt.Errorf("%v names no query", config.Endpoint)
	}

	return &astralAuthorizer{
		node: mod.node,
		// why a closure and not the module itself: the authorizers are built at
		// load and dependencies are injected after, so mod.Dir is read when the
		// first question is asked rather than when this is built.
		resolve: func(name string) (*astral.Identity, error) {
			return mod.Dir.ResolveIdentity(name)
		},
		name: target,
		path: path,
	}, nil
}

func (a *astralAuthorizer) String() string { return astralScheme + a.name + ":" + a.path }

// Ask sends the action to the authority and reads one object back. The context bounds the send and the receive.
func (a *astralAuthorizer) Ask(ctx *astral.Context, action auth.ActionObject) (bool, error) {
	target, err := a.resolveTarget()
	if err != nil {
		return false, err
	}

	// why: an authority asked whether it may act would have to answer the
	// question before it could answer the question.
	if action.Actor().IsEqual(target) {
		return false, errors.New("the authority cannot be the actor")
	}

	conn, err := a.route(ctx, target)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	// why: a send and a receive have no deadline of their own, so an authority that accepts the query and never
	// reads or never answers would hold the authorization past its timeout.
	stop := closeWhenDone(ctx, conn)
	defer stop()

	ch := channel.New(conn)
	if err = ch.Send(action); err != nil {
		return false, contextErr(ctx, err)
	}

	obj, err := ch.Receive()
	if err != nil {
		return false, contextErr(ctx, err)
	}

	// why ack alone: every other object is an answer this code cannot read, and
	// an unreadable answer must not read as permission.
	_, allow := obj.(*astral.Ack)

	return allow, nil
}

// route routes the question to the target and returns the asker's end of it.
//
// why not query.Route: the asker keeps the read end of the pipe, so closing the connection also ends a blocked
// receive.
func (a *astralAuthorizer) route(ctx *astral.Context, target *astral.Identity) (*questionConn, error) {
	reader, writer := io.Pipe()

	w, err := a.node.RouteQuery(ctx, astral.Launch(query.New(a.node.Identity(), target, a.path, nil)), writer)
	if err != nil {
		reader.Close()
		return nil, err
	}

	return &questionConn{PipeReader: reader, WriteCloser: w}, nil
}

// questionConn is the asker's end of a question. Close closes the read end and the write end, once.
type questionConn struct {
	*io.PipeReader
	io.WriteCloser
	closed atomic.Bool
}

func (c *questionConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.PipeReader.Close()

	return c.WriteCloser.Close()
}

// closeWhenDone closes c when ctx ends. The returned stop returns only after a close it started has finished, so
// no goroutine outlives the caller.
func closeWhenDone(ctx context.Context, c io.Closer) (stop func()) {
	closed := make(chan struct{})
	stopAfter := context.AfterFunc(ctx, func() {
		defer close(closed)
		c.Close()
	})

	return func() {
		if !stopAfter() {
			<-closed
		}
	}
}

// contextErr returns the context's error when the context has ended, and err otherwise.
func contextErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	return err
}

// resolveTarget resolves the configured name into an identity, once.
func (a *astralAuthorizer) resolveTarget() (*astral.Identity, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.target != nil {
		return a.target, nil
	}

	id, err := a.resolve(a.name)
	if err != nil {
		return nil, fmt.Errorf("resolve authority %v: %w", a.name, err)
	}
	if id == nil || id.IsZero() {
		return nil, fmt.Errorf("authority %v resolves to no identity", a.name)
	}

	a.target = id

	return id, nil
}
