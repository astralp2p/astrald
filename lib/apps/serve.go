package apps

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/astral"
	libastrald "github.com/cryptopunkscc/astrald/lib/astrald"
)

type ServeOption func(*serveConfig) error

type serveConfig struct {
	mounts []func(astral.Router) error
	hooks  []RegistrationHook
}

// Serve creates a handler, registers it with the default session, gates queries until
// registered, and routes all inbound queries to router. Blocks until ctx is cancelled.
func Serve(ctx *astral.Context, router astral.Router, opts ...ServeOption) error {
	cfg, err := newServeConfig(opts...)
	if err != nil {
		return err
	}

	if err := applyMounts(router, cfg); err != nil {
		return err
	}

	reg := NewDefaultAppRegistrar(ctx, WithRegistrarRegistrationHooks(cfg.hooks...))
	return serveRegistered(ctx, router, reg)
}

// ServeWith is like Serve but with an explicit registrar.
// If reg also implements libastrald.ReadyGate, queries are gated until it signals ready.
func ServeWith(ctx *astral.Context, router astral.Router, reg Registrar, opts ...ServeOption) error {
	cfg, err := newServeConfig(opts...)
	if err != nil {
		return err
	}

	if err := applyMounts(router, cfg); err != nil {
		return err
	}

	if err := applyServeConfig(reg, cfg); err != nil {
		return err
	}

	return serveRegistered(ctx, router, reg)
}

// serveRegistered starts the IPC route loop before registering the handler with the node so
// that registration hooks (e.g. blueprint sync) can issue queries that the node may route
// back through this handler. Without this ordering, the registrar pushes the handler into
// the node's ipcHandlers list, then runs the hook synchronously — and if the hook's query
// targets the host identity that equals this guest's identity, the node dials this listener
// and blocks waiting for an Ack that the not-yet-started Route loop can't provide.
func serveRegistered(ctx *astral.Context, router astral.Router, reg Registrar) error {
	h, err := NewHandler()
	if err != nil {
		return err
	}
	defer h.Close()

	if rg, ok := reg.(libastrald.ReadyGate); ok {
		router = NewGateRouter(router, rg)
	}

	routeErrCh := make(chan error, 1)
	go func() {
		routeErrCh <- h.Route(ctx, router)
	}()

	if err := reg.Register(ctx, h.Endpoint(), h.Token()); err != nil {
		return err
	}

	return <-routeErrCh
}

func WithRegistrationHook(hook RegistrationHook) ServeOption {
	return WithRegistrationHooks(hook)
}

// WithRegistrationHooks adds hooks that run after each successful (re)registration with the node.
func WithRegistrationHooks(hooks ...RegistrationHook) ServeOption {
	return func(cfg *serveConfig) error {
		for _, hook := range hooks {
			if hook == nil {
				return errors.New("nil registration hook")
			}
			cfg.hooks = append(cfg.hooks, hook)
		}
		return nil
	}
}

func newServeConfig(opts ...ServeOption) (*serveConfig, error) {
	cfg := &serveConfig{}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("nil serve option")
		}
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func applyMounts(router astral.Router, cfg *serveConfig) error {
	for _, mount := range cfg.mounts {
		if mount == nil {
			return errors.New("nil route mount")
		}
		if err := mount(router); err != nil {
			return err
		}
	}
	return nil
}

func applyServeConfig(reg Registrar, cfg *serveConfig) error {
	if len(cfg.hooks) == 0 {
		return nil
	}
	hookReg, ok := reg.(RegistrationHookRegistrar)
	if !ok {
		return errors.New("registrar does not support registration hooks")
	}
	hookReg.AddRegistrationHooks(cfg.hooks...)
	return nil
}
