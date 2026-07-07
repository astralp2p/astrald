package tor

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/tor"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/mod/tor/tc"
)

// Server manages the Tor hidden-service lifecycle for the module, including its advertised onion endpoint.
type Server struct {
	*Module
	endpoint *tor.Endpoint
}

func NewServer(module *Module) *Server {
	return &Server{Module: module}
}

// Run acquires or generates the node's private key, registers a Tor onion service via the control port,
// and dispatches each accepted connection to the nodes layer for inbound link establishment.
func (srv *Server) Run(ctx *astral.Context) error {
	key, err := srv.getPrivateKey()
	if err != nil {
		srv.log.Error("getPrivateKey: %v", err)
		return err
	}

	l, err := srv.listen(ctx, key)
	if err != nil {
		srv.log.Error("listen: %v", err)
		return err
	}
	defer l.Close()

	srv.endpoint, err = Parse(l.Addr())
	if err != nil {
		srv.log.Errorv(1, "error parsing tor key: %v", err)
	}

	srv.log.Log("listen %v", srv.endpoint)

	for {
		rawConn, err := l.Accept()
		switch {
		case err == nil:
		case strings.Contains(err.Error(), "use of closed network connection"):
			return err
		default:
			srv.log.Error("accept: %v", err)
			return err
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

func (srv *Server) listen(ctx context.Context, key Key) (*listener, error) {
	// Set up the listener for incoming tor connections
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	sig.On(ctx, func() {
		tcpListener.Close()
	})

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

	onion, err := ctl.AddOnion(keyStr, tc.Port(srv.config.ListenPort, tcpListener.Addr().String()))
	if err != nil {
		return nil, err
	}

	return &listener{tcpListener, onion, ctl}, nil
}

func (srv *Server) control() (*tc.Control, error) {
	conn, err := srv.connect()
	if err != nil {
		return nil, err
	}

	ctl := tc.New(conn)
	if err := ctl.Authenticate(); err != nil {
		return nil, err
	}

	return ctl, nil
}

func (srv *Server) connect() (io.ReadWriteCloser, error) {
	return net.Dial("tcp", srv.config.ControlAddr)
}

type listener struct {
	tcp   net.Listener
	onion tc.Onion
	ctl   *tc.Control
}

func (l listener) Addr() string {
	return l.onion.ServiceID
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
	return l.tcp.Accept()
}

func (l listener) Close() error {
	l.tcp.Close()

	return l.ctl.DelOnion(l.onion.ServiceID)
}

func (mod *Module) startServer(ctx context.Context) {
	if mod.torServer == nil {
		return
	}
	if err := mod.torServer.Run(astral.NewContext(ctx)); err != nil {
		mod.log.Errorv(1, "server error: %v", err)
	}
}
