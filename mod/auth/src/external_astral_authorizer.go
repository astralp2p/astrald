package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
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

// Ask sends the action to the authority and reads one object back.
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

	ch, err := query.Route(ctx, a.node, query.New(a.node.Identity(), target, a.path, nil))
	if err != nil {
		return false, err
	}
	defer ch.Close()

	if err = ch.Send(action); err != nil {
		return false, err
	}

	obj, err := ch.Receive()
	if err != nil {
		return false, err
	}

	// why ack alone: every other object is an answer this code cannot read, and
	// an unreadable answer must not read as permission.
	_, allow := obj.(*astral.Ack)

	return allow, nil
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
