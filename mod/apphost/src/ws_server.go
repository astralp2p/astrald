package apphost

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

const (
	SubprotocolBinary = "astral.binary.v1"
	SubprotocolJSON   = "astral.json.v1"
)

// handleWS upgrades a request on /.ws to a WebSocket connection speaking the apphost
// protocol. Mode is selected by the negotiated Sec-WebSocket-Protocol:
//
//   - astral.binary.v1: binary frames carrying the existing String8+Bytes32 byte stream
//   - astral.json.v1:   text frames carrying one JSON-encoded astral.Object per frame
//
// Auth is in-protocol via AuthTokenMsg (matching the native protocol), not via the
// HTTP Authorization header used by the rest of HTTPServer.
func (srv *HTTPServer) handleWS(writer http.ResponseWriter, request *http.Request) {
	conn, mode, chFmt := srv.negotiateWS(writer, request)
	if conn == nil {
		return
	}

	ctx := srv.ctx
	if ctx == nil {
		ctx = astral.NewContext(nil)
	}

	ch := channel.New(conn,
		channel.WithFormats(chFmt, chFmt),
		channel.WithLockedWrites(),
	)

	// close the WS when the module context ends
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	guest := NewGuestFromChannel(srv.Module, ch, conn, mode)
	// Record the web Origin (if any) so routed queries can carry it for
	// op-level filtering. Empty for non-browser WS clients.
	guest.webOrigin = request.Header.Get("Origin")
	defer func() {
		// If the guest donated its conn to a routing goroutine via AttachQueryMsg,
		// that goroutine now owns the close. Otherwise close here.
		if !guest.donated.Load() {
			conn.Close()
		}
	}()
	err := guest.Serve(ctx)

	switch {
	case err == nil:
	case errors.Is(err, io.EOF):
	case errors.Is(err, context.Canceled):
		// streams.Join's trailing goroutine returns this when its peer goroutine
		// closes the wsConn (which cancels the read ctx) — clean end-of-stream
		// for our adapter. Also fires on normal module shutdown.
	case websocket.CloseStatus(err) != -1:
		// Peer sent a Close frame — normal WS termination.
	case strings.Contains(err.Error(), "use of closed network connection"):
	case strings.Contains(err.Error(), "connection closed"):
	case strings.Contains(err.Error(), "read/write on closed pipe"):
	case strings.Contains(err.Error(), "received close frame"):
	default:
		srv.log.Error("ws serve error: %v", err)
	}
}

// negotiateWS handles the WebSocket upgrade and subprotocol selection. Returns a usable
// conn and the matching Mode/channel format on success, or (nil, 0, "") if the upgrade
// was refused (in which case an HTTP error response or WS close has already been written).
func (srv *HTTPServer) negotiateWS(writer http.ResponseWriter, request *http.Request) (*wsConn, Mode, string) {
	if !isLoopback(request) {
		http.Error(writer, "websocket endpoint is loopback-only", http.StatusForbidden)
		return nil, 0, ""
	}

	// Accept any Origin. The endpoint is already loopback-only at the network
	// layer (isLoopback above); per-caller origin filtering for web pages is
	// done at the op level via the ExtraOriginWeb key added to each query's Extra
	// (see Guest.onRouteQueryMsg).
	c, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:       []string{SubprotocolBinary, SubprotocolJSON},
		InsecureSkipVerify: true,
	})
	if err != nil {
		// websocket.Accept already wrote an HTTP error response
		return nil, 0, ""
	}

	var (
		mode    Mode
		msgType websocket.MessageType
		chFmt   string
	)
	switch c.Subprotocol() {
	case SubprotocolBinary:
		mode, msgType, chFmt = ModeBinary, websocket.MessageBinary, channel.Binary
	case SubprotocolJSON:
		mode, msgType, chFmt = ModeJSON, websocket.MessageText, channel.JSON
	default:
		c.Close(websocket.StatusPolicyViolation, "client must request a known subprotocol")
		return nil, 0, ""
	}

	ctx := srv.ctx
	if ctx == nil {
		ctx = astral.NewContext(nil)
	}

	return newWSConn(ctx, c, msgType), mode, chFmt
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
