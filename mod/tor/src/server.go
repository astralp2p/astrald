package tor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/ipc"
	"github.com/astralp2p/astrald/mod/tor/tc"
)

// Server manages the Tor hidden-service lifecycle for the module, including its advertised onion endpoint.
type Server struct {
	*Module
	endpoint atomic.Pointer[tor.Endpoint]
}

func NewServer(module *Module) *Server {
	return &Server{Module: module}
}

// errServiceDown wraps the error that ends a Run after the onion service was registered.
var errServiceDown = errors.New("onion service down")

// Endpoint returns the onion endpoint the service is registered on, or nil while it is down.
func (srv *Server) Endpoint() *tor.Endpoint {
	return srv.endpoint.Load()
}

// Run acquires or generates the node's private key, registers a Tor onion service via the control port,
// and dispatches each accepted connection to the nodes layer for inbound link establishment.
// Run returns when ctx ends, or with an errServiceDown error when the control connection that owns
// the service closes.
func (srv *Server) Run(ctx *astral.Context) error {
	key, err := srv.getPrivateKey()
	if err != nil {
		return fmt.Errorf("private key: %w", err)
	}

	l, err := srv.listen(key)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer l.Close()

	stop := context.AfterFunc(ctx, func() { l.target.Close() })
	defer stop()

	// why: Tor removes a service registered without Detach when its control connection closes, as
	// on a Tor restart. ctlClosed is filled before the target closes, so the accept loop sees it.
	ctlClosed := make(chan error, 1)
	go func() {
		ctlClosed <- l.ctl.Wait()
		l.target.Close()
	}()

	endpoint, err := l.Endpoint(srv.config.ListenPort)
	if err != nil {
		srv.log.Errorv(1, "error parsing tor key: %v", err)
	} else {
		srv.endpoint.Store(endpoint)
	}

	srv.log.Log("listen %v", endpoint)

	return srv.accept(ctx, l, ctlClosed)
}

// accept dispatches each connection Tor forwards to the nodes layer until the target listener
// closes. A close caused by the control connection is logged as the loss of the service.
func (srv *Server) accept(ctx *astral.Context, l *listener, ctlClosed <-chan error) error {
	for {
		rawConn, err := l.Accept()
		if err != nil {
			endpoint := srv.endpoint.Swap(nil)
			select {
			case cerr := <-ctlClosed:
				srv.log.Log("onion service %v is down: tor control connection closed: %v", endpoint, cerr)
				return fmt.Errorf("%w: tor control connection closed: %w", errServiceDown, cerr)
			default:
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			srv.log.Error("accept: %v", err)
			return fmt.Errorf("%w: accept: %w", errServiceDown, err)
		}

		var conn = newConn(rawConn, &tor.Endpoint{}, false)
		go func() {
			err := srv.Nodes.EstablishInboundLink(ctx, conn)
			if err != nil {
				srv.log.Errorv(1, "handshake failed from %v: %v", conn.RemoteEndpoint(), err)
			}
		}()
	}
}

// listen registers an onion service for key, or for a new key when key is empty. On error it
// leaves no listener or control connection open.
func (srv *Server) listen(key Key) (_ *listener, err error) {
	// Set up the listener for incoming tor connections
	targetListener, err := ipc.Listen(ipcAddr(srv.config.OnionTarget))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			targetListener.Close()
		}
	}()

	ctl, err := srv.control()
	if err != nil {
		return nil, err
	}

	var keyStr string

	if len(key) == 0 {
		keyStr = tc.KeyNewV3
	} else {
		keyStr = "ED25519-V3:" + base64.StdEncoding.EncodeToString(key)
	}

	onion, err := ctl.AddOnion(keyStr, tc.Port(srv.config.ListenPort, onionTarget(targetListener)))
	if err != nil {
		ctl.Close()
		return nil, err
	}

	return &listener{targetListener, onion, ctl}, nil
}

// onionTarget is the target ADD_ONION names for l: a TCP listener's host:port, or a unix
// listener's path as "unix:/path".
func onionTarget(l net.Listener) string {
	if l.Addr().Network() == "unix" {
		return unixPrefix + l.Addr().String()
	}

	return l.Addr().String()
}

func (srv *Server) control() (*tc.Control, error) {
	network, address := netAddr(srv.config.ControlAddr)

	conn, err := srv.connect(network, address)
	if err != nil {
		return nil, err
	}

	ctl := tc.New(conn)

	authenticate := ctl.Authenticate
	if network == "unix" {
		authenticate = ctl.AuthenticateOverUnixSocket
	}
	if err := authenticate(); err != nil {
		conn.Close()
		return nil, err
	}

	return ctl, nil
}

func (srv *Server) connect(network, address string) (io.ReadWriteCloser, error) {
	return net.Dial(network, address)
}

type listener struct {
	target net.Listener
	onion  tc.Onion
	ctl    *tc.Control
}

func (l listener) Addr() string {
	return l.onion.ServiceID
}

// Endpoint returns the onion endpoint for the virtual port the service was registered on.
func (l listener) Endpoint(port int) (*tor.Endpoint, error) {
	return Parse(l.Addr() + ":" + strconv.Itoa(port))
}

func (l listener) PrivateKey() Key {
	s := l.onion.PrivateKey

	// force v3 as v2 is now considered insecure
	if !strings.HasPrefix(s, "ED25519-V3:") {
		return nil
	}

	ss := strings.Split(s, ":")

	if len(ss) != 2 {
		return nil
	}

	key, err := base64.StdEncoding.DecodeString(ss[1])
	if err != nil {
		return nil
	}

	return key
}

func (l listener) Accept() (net.Conn, error) {
	return l.target.Accept()
}

// Close closes the target listener and the control connection.
//
// why: Tor removes a service registered without Detach together with the control connection that
// owns it, so no DEL_ONION is sent. A DEL_ONION reply would also race Control.Wait for the transport.
func (l listener) Close() error {
	l.target.Close()

	return l.ctl.Close()
}

// retryMin and retryMax bound the backoff between attempts to run the onion service.
var (
	retryMin = time.Second
	retryMax = time.Minute
)

// startServer runs the onion service until ctx ends. A failed start is retried with backoff that
// doubles from retryMin to retryMax; a service that was registered and went down is retried after
// retryMin.
func (mod *Module) startServer(ctx context.Context) {
	if mod.torServer == nil {
		return
	}

	delay, last := retryMin, ""
	for {
		err := mod.torServer.Run(astral.NewContext(ctx))
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errServiceDown) {
			delay = retryMin
		}

		// why: Tor may stay unreachable for hours, so a repeated error is verbose and a new one is not.
		level := 1
		if err.Error() != last {
			level, last = 0, err.Error()
		}
		mod.log.Errorv(level, "server error: %v; retrying in %v", err, delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		delay = min(delay*2, retryMax)
	}
}
