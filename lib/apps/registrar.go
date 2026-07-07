package apps

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	libastrald "github.com/cryptopunkscc/astrald/lib/astrald"
	"github.com/cryptopunkscc/astrald/mod/apphost"
	apphostclient "github.com/cryptopunkscc/astrald/mod/apphost/client"
)

// Registrar registers an IPC endpoint with the node.
type Registrar interface {
	Register(ctx *astral.Context, endpoint string, token astral.Nonce) error
}

// AppRegistrarEvents are optional lifecycle callbacks.
// NodeBinding was an alternative name considered for AppRegistrar.
type AppRegistrarEvents struct {
	OnConnect    func()
	OnDisconnect func(error)
}

type handler struct {
	endpoint string
	token    astral.Nonce
}

type regRequest struct {
	handler
	done chan error
}

// RegistrationHook is called after every successful (re)connect and handler re-registration.
// Errors abort the reconnect cycle and trigger a reconnect attempt.
type RegistrationHook func(ctx *astral.Context) error

// RegistrationHookRegistrar is an optional extension of Registrar for components that support
// post-registration lifecycle hooks.
type RegistrationHookRegistrar interface {
	AddRegistrationHooks(hooks ...RegistrationHook)
}

// AppRegistrar maintains a persistent bind channel to the node, re-registers all
// handlers on reconnect, and signals readiness. Implements Registrar and ReadyGate.
type AppRegistrar struct {
	client        *apphostclient.Client
	dial          NodeBind
	attachHandler func(*astral.Context, *channel.Channel, handler) error
	events        AppRegistrarEvents
	ready         sig.Value[chan struct{}]
	handlers      []handler
	hooks         []RegistrationHook
	regCh         chan *regRequest
}

var _ Registrar = &AppRegistrar{}
var _ libastrald.ReadyGate = &AppRegistrar{}
var _ RegistrationHookRegistrar = &AppRegistrar{}

type AppRegistrarOption func(*AppRegistrar)

// WithClient sets the apphost client and derives the node bind from it.
func WithClient(c *apphostclient.Client) AppRegistrarOption {
	return func(s *AppRegistrar) {
		s.client = c
		s.dial = c.Bind
	}
}

// WithBind overrides the node bind function.
func WithBind(d NodeBind) AppRegistrarOption { return func(s *AppRegistrar) { s.dial = d } }

// WithEvents sets lifecycle callbacks.
func WithEvents(e AppRegistrarEvents) AppRegistrarOption {
	return func(s *AppRegistrar) { s.events = e }
}

// WithRegistrarRegistrationHooks adds RegistrationHooks at construction time.
// Use WithRegistrationHooks (a ServeOption) to add hooks via Serve/ServeWith instead.
func WithRegistrarRegistrationHooks(hooks ...RegistrationHook) AppRegistrarOption {
	return func(s *AppRegistrar) { s.AddRegistrationHooks(hooks...) }
}

// NewAppRegistrar creates an AppRegistrar and starts its run loop with the given ctx.
func NewAppRegistrar(ctx *astral.Context, opts ...AppRegistrarOption) *AppRegistrar {
	c := apphostclient.Default()
	s := &AppRegistrar{
		client: c,
		dial:   NodeBind(c.Bind),
		regCh:  make(chan *regRequest),
	}
	s.attachHandler = s.attach
	s.ready.Set(make(chan struct{}))
	for _, o := range opts {
		o(s)
	}
	go s.run(ctx)
	return s
}

func NewDefaultAppRegistrar(ctx *astral.Context, opts ...AppRegistrarOption) *AppRegistrar {
	return NewAppRegistrar(ctx, opts...)
}

func (s *AppRegistrar) Ready() <-chan struct{} { return s.ready.Get() }

func (s *AppRegistrar) AddRegistrationHooks(hooks ...RegistrationHook) {
	for _, hook := range hooks {
		if hook != nil {
			s.hooks = append(s.hooks, hook)
		}
	}
}

func (s *AppRegistrar) Register(ctx *astral.Context, endpoint string, token astral.Nonce) error {
	req := &regRequest{handler{endpoint, token}, make(chan error, 1)}
	if err := sig.Send(ctx, s.regCh, req); err != nil {
		return err
	}
	return sig.RecvErr(ctx, req.done)
}

func (s *AppRegistrar) run(ctx *astral.Context) {
	for {
		bindCh, err := s.dial(ctx)
		if err != nil {
			return
		}

		if err = s.attachAll(ctx, bindCh); err != nil {
			bindCh.Close()
			if ctx.Err() != nil {
				return
			}
			if s.events.OnDisconnect != nil {
				s.events.OnDisconnect(err)
			}
			continue
		}

		close(s.ready.Get())
		if s.events.OnConnect != nil {
			s.events.OnConnect()
		}

		reconnect, disconnectErr := s.loop(ctx, bindCh)
		bindCh.Close()

		if !reconnect {
			return
		}

		s.ready.Set(make(chan struct{}))
		if s.events.OnDisconnect != nil {
			s.events.OnDisconnect(disconnectErr)
		}
	}
}

func (s *AppRegistrar) attach(ctx *astral.Context, bindCh *channel.Channel, h handler) error {
	if err := s.client.RegisterHandler(ctx, h.endpoint, h.token); err != nil {
		return err
	}

	return bindCh.Send(&apphost.BindMsg{Token: h.token})
}

func (s *AppRegistrar) attachAll(ctx *astral.Context, bindCh *channel.Channel) error {
	if len(s.handlers) == 0 {
		return nil
	}

	for _, h := range s.handlers {
		if err := s.attachHandler(ctx, bindCh, h); err != nil {
			return err
		}
	}

	return s.runHooks(ctx)
}

func (s *AppRegistrar) attachRegistered(ctx *astral.Context, bindCh *channel.Channel, h handler) error {
	if err := s.attachHandler(ctx, bindCh, h); err != nil {
		return err
	}

	s.handlers = append(s.handlers, h)
	if err := s.runHooks(ctx); err != nil {
		s.handlers = s.handlers[:len(s.handlers)-1]
		return err
	}
	return nil
}

func (s *AppRegistrar) runHooks(ctx *astral.Context) error {
	for _, hook := range s.hooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *AppRegistrar) loop(ctx *astral.Context, bindCh *channel.Channel) (reconnect bool, disconnectErr error) {
	disconnected := make(chan error, 1)
	go func() {
		for {
			if _, err := bindCh.Receive(); err != nil {
				disconnected <- err
				return
			}
		}
	}()

	for {
		select {
		case req := <-s.regCh:
			if err := s.attachRegistered(ctx, bindCh, req.handler); err != nil {
				req.done <- err
				return false, nil
			}
			req.done <- nil

		case disconnectErr = <-disconnected:
			return true, disconnectErr

		case <-ctx.Done():
			return false, nil
		}
	}
}
