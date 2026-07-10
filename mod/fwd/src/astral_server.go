package fwd

import (
	"errors"
	"io"
	"strings"

	"github.com/astralp2p/astral-go/astral"
)

// AstralServer listens on an astral service name and forwards queries to a target router.
// note: Run currently always returns an error; the service-registration path is commented out.
type AstralServer struct {
	*Module
	serviceName string
	identity    *astral.Identity
	target      astral.Router
}

// NewAstralServer parses serviceName as [identity@]service, resolving the optional identity prefix via Dir.
func NewAstralServer(mod *Module, serviceName string, target astral.Router) (*AstralServer, error) {
	var err error
	var identity = mod.node.Identity()
	var srv = &AstralServer{
		Module: mod,
		target: target,
	}

	if idx := strings.Index(serviceName, "@"); idx != -1 {
		name := serviceName[:idx]
		identity, err = mod.Dir.ResolveIdentity(name)
		if err != nil {
			return nil, err
		}

		serviceName = serviceName[idx+1:]
	}

	srv.identity = identity
	srv.serviceName = serviceName

	return srv, nil
}

// Run always returns an error; service registration is not yet implemented.
func (srv *AstralServer) Run(ctx *astral.Context) error {
	return errors.New("obsolete")
	//var err = srv.AddOp(srv.serviceName, routing.NewOp())
	//if err != nil {
	//	return err
	//}
	//defer srv.RemoveOp(srv.serviceName)
	//
	//<-ctx.Done()
	//
	//return nil
}

func (srv *AstralServer) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	dst, err := srv.target.RouteQuery(ctx, q, w)
	if err != nil {
		return nil, err
	}

	return dst, nil
}

func (srv *AstralServer) Target() astral.Router {
	return srv.target
}

func (srv *AstralServer) String() string {
	return "astral://" + srv.serviceName
}
