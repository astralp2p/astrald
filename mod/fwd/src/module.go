package fwd

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/tcp"
	"github.com/astralp2p/astrald/mod/tor"
)

// Deps holds optional module dependencies; Tor may be nil when the tor driver is absent.
type Deps struct {
	Dir dir.Module
	TCP tcp.Module
	Tor tor.Module
}

type Module struct {
	Deps
	*routing.OpRouter
	node    astral.Node
	config  Config
	log     *log.Logger
	ctx     *astral.Context
	servers map[*ServerRunner]struct{}
	mu      sync.Mutex
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	for server, target := range mod.config.Forwards {
		err := mod.CreateForward(server, target)
		if err != nil {
			mod.log.Errorv(1, "error creating %v -> %v: %v",
				server,
				target,
				err,
			)
		}
	}

	<-ctx.Done()
	mod.waitForServers()

	return nil
}

// CreateForward parses the server and target URIs, constructs the appropriate server, and starts it in a goroutine.
func (mod *Module) CreateForward(server, target string) error {
	t, err := mod.parseTarget(target)
	if err != nil {
		return fmt.Errorf("cannot parse target: %w", err)
	}

	s, err := mod.createServer(server, t)
	if err != nil {
		return fmt.Errorf("cannot create server: %w", err)
	}

	return mod.runServer(s)
}

func (mod *Module) Servers() []*ServerRunner {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	var list = make([]*ServerRunner, 0)

	for server := range mod.servers {
		list = append(list, server)
	}

	return list
}

func (mod *Module) waitForServers() {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	for server := range mod.servers {
		<-server.Done()
	}
}

func (mod *Module) runServer(s *ServerRunner) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	mod.servers[s] = struct{}{}

	go func() {
		defer s.Stop()

		err := s.Run(s.ctx)

		if err != nil {
			mod.log.Errorv(1, "%v ended with error: %v", s, err)
		}

		mod.mu.Lock()
		defer mod.mu.Unlock()

		delete(mod.servers, s)
	}()

	return nil
}

func (mod *Module) parseTarget(uri string) (astral.Router, error) {
	var err error
	var idx = strings.Index(uri, "://")
	if idx == -1 {
		return nil, errors.New("missing protocol")
	}

	var proto = uri[:idx]
	uri = uri[idx+3:]

	switch proto {
	case "tcp":
		return NewTCPTarget(uri, mod.node.Identity())

	case "astral":
		var caller = mod.node.Identity()
		var target = mod.node.Identity()

		if idx := strings.Index(uri, "@"); idx != -1 {
			callerName := uri[:idx]
			uri = uri[idx+1:]

			caller, err = mod.Dir.ResolveIdentity(callerName)
			if err != nil {
				return nil, err
			}
		}

		if idx := strings.Index(uri, ":"); idx != -1 {
			name := uri[:idx]
			uri = uri[idx+1:]

			target, err = mod.Dir.ResolveIdentity(name)
			if err != nil {
				return nil, err
			}
		}

		if len(uri) == 0 {
			return nil, errors.New("missing query")
		}

		var query = astral.NewQuery(caller, target, uri)

		var label = fmt.Sprintf("%s@%s:%s",
			mod.Dir.DisplayName(caller),
			mod.Dir.DisplayName(target),
			uri,
		)

		return NewAstralTarget(query, mod.node, label)

	case "tor":
		if mod.Tor == nil {
			return nil, errors.New("tor driver not found")
		}

		return NewTorTarget(mod.Tor, uri, mod.node.Identity())

	default:
		return nil, errors.New("unsupported protocol")
	}
}

func (mod *Module) createServer(uri string, target astral.Router) (*ServerRunner, error) {
	var idx = strings.Index(uri, "://")
	if idx == -1 {
		return nil, errors.New("missing protocol")
	}

	var proto = uri[:idx]
	uri = uri[idx+3:]

	switch proto {
	case "tcp":
		tcpServer, err := NewTCPServer(mod, uri, target)
		if err != nil {
			return nil, err
		}

		return NewServerRunner(mod.ctx, tcpServer), nil

	case "astral":
		astralServer, err := NewAstralServer(mod, uri, target)
		if err != nil {
			return nil, err
		}

		return NewServerRunner(mod.ctx, astralServer), nil

	default:
		return nil, errors.New("unsupported protocol")
	}
}

func (mod *Module) String() string {
	return ModuleName
}
