package mcp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/ipc"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer serves MCP over streamable HTTP. Agents authenticate with their
// PAT as a bearer token; every tool acts as the authenticated identity.
type MCPServer struct {
	*Module
}

func NewMCPServer(mod *Module) *MCPServer {
	return &MCPServer{Module: mod}
}

// Run binds the MCP listener and blocks until ctx is done.
// Returns nil immediately when BindMCP is empty (MCP disabled).
func (srv *MCPServer) Run(ctx *astral.Context) error {
	bind := srv.config.BindMCP
	if bind == "" {
		return nil
	}

	l, err := ipc.Listen(bind)
	if err != nil {
		srv.log.Error("mcp server: bind %v: %v", bind, err)
		return err
	}

	httpServer := &http.Server{Handler: srv.handler()}

	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(sctx); err != nil {
			srv.log.Error("mcp server: shutdown error at %v: %v", bind, err)
		} else {
			srv.log.Logv(1, "mcp server: stopped at %v", bind)
		}
	}()

	srv.log.Infov(1, "mcp server: started at %v", bind)
	err = httpServer.Serve(l)
	switch {
	case err == nil:
	case errors.Is(err, http.ErrServerClosed):
	default:
		srv.log.Error("mcp server: serve error at %v: %v", bind, err)
	}

	<-ctx.Done()
	return nil
}

// handler builds the middleware stack: bearer auth wrapping the streamable
// MCP handler.
func (srv *MCPServer) handler() http.Handler {
	return sdkauth.RequireBearerToken(srv.verifyToken, nil)(
		mcpsdk.NewStreamableHTTPHandler(srv.getServer, &mcpsdk.StreamableHTTPOptions{
			SessionTimeout: 30 * time.Minute,
		}),
	)
}

// verifyToken resolves a bearer PAT to an identity via apphost access tokens.
// Any valid apphost token authenticates — tokens are host-issued either way.
func (srv *MCPServer) verifyToken(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	id, err := srv.Apphost.AuthenticateToken(token)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}

	return &sdkauth.TokenInfo{
		UserID: id.String(),
		Extra:  map[string]any{"identity": id},
		// why: apphost re-checks real token expiry on every request; this
		// stamp only satisfies the middleware's freshness check.
		Expiration: time.Now().Add(time.Minute),
	}, nil
}

// getServer builds a per-session MCP server with the tools bound to the
// authenticated agent identity.
func (srv *MCPServer) getServer(req *http.Request) *mcpsdk.Server {
	info := sdkauth.TokenInfoFromContext(req.Context())
	if info == nil {
		return nil // note: nil yields 400; RequireBearerToken runs first
	}

	agentID, ok := info.Extra["identity"].(*astral.Identity)
	if !ok {
		return nil
	}

	s := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "astrald",
		Title:   "Astral Network",
		Version: "0.1.0",
	}, nil)

	srv.addTools(s, agentID)

	return s
}
