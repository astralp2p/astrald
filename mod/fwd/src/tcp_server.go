package fwd

import (
	"io"
	_net "net"
	"strings"

	"github.com/astralp2p/astral-go/astral"
)

var _ Server = &TCPServer{}

// TCPServer accepts TCP connections and forwards each as a query to the configured target router.
type TCPServer struct {
	*Module
	bind     string
	target   astral.Router
	listener _net.Listener
}

// NewTCPServer binds the TCP listener immediately at construction; the port is held before Run is called.
func NewTCPServer(mod *Module, bind string, target astral.Router) (*TCPServer, error) {
	var err error
	var srv = &TCPServer{
		Module: mod,
		target: target,
		bind:   bind,
	}

	srv.listener, err = _net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}

	return srv, nil
}

func (srv *TCPServer) Run(ctx *astral.Context) error {
	go func() {
		<-ctx.Done()
		srv.listener.Close()
	}()

	for {
		client, err := srv.listener.Accept()
		if err != nil {
			switch {
			case strings.Contains(err.Error(), "use of closed network connection"):
				return nil
			default:
				return err
			}
		}

		go func() {
			var q = astral.NewQuery(nil, nil, "")

			dst, err := srv.target.RouteQuery(ctx, astral.Launch(q), client)
			if err != nil {
				client.Close()
				return
			}
			defer dst.Close()

			io.Copy(dst, client)
		}()
	}
}

func (srv *TCPServer) Target() astral.Router {
	return srv.target
}

func (srv *TCPServer) String() string {
	return "tcp://" + srv.bind
}
