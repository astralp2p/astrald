package apphost

import (
	"context"
	"errors"
	apphostmod "github.com/cryptopunkscc/astrald/mod/apphost"
	authmod "github.com/cryptopunkscc/astrald/mod/auth"
	"io"
	"net"
	"strings"
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/streams"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Mode is the wire format used by a Guest connection.
type Mode int

const (
	ModeBinary Mode = iota
	ModeJSON
)

// Guest represents an active connection with a guest.
type Guest struct {
	*channel.Channel
	mod       *Module
	mode      Mode
	guestID   *astral.Identity
	conn      io.ReadWriteCloser
	webOrigin string      // browser Origin header for WS guests; "" for TCP/unix/memu
	donated   atomic.Bool // set when the conn has been donated to a routing goroutine
}

type queryEnRoute struct {
	query  *astral.InFlightQuery
	cancel context.CancelCauseFunc
}

// NewGuest creates a binary-mode Guest over a net.Conn. Used by TCP/unix/memu listeners.
func NewGuest(mod *Module, conn net.Conn) *Guest {
	return NewGuestFromChannel(mod, channel.New(conn), conn, ModeBinary)
}

// NewGuestFromChannel creates a Guest over a pre-built channel. The closer is what
// gets closed when Serve returns; in the WS path it's the same object that backs the channel.
func NewGuestFromChannel(mod *Module, ch *channel.Channel, conn io.ReadWriteCloser, mode Mode) *Guest {
	return &Guest{
		mod:     mod,
		conn:    conn,
		Channel: ch,
		mode:    mode,
	}
}

// Serve handles the guest connection.
func (guest *Guest) Serve(ctx *astral.Context) (err error) {
	// write a welcome message
	err = guest.Send(&apphost.HostInfoMsg{
		Identity: ctx.Identity(),
		Alias:    astral.String8(guest.mod.Dir.DisplayName(ctx.Identity())),
	})
	if err != nil {
		return
	}

	// message read loop
	var msg astral.Object
	for {
		msg, err = guest.Receive()

		// check err
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return nil
		case strings.Contains(err.Error(), "use of closed network connection"):
			return nil
		case strings.Contains(err.Error(), "received close frame"):
			// Normal WS termination by the peer.
			return nil
		default:
			guest.mod.log.Logv(2, "error reading from client: %v", err)
			return err
		}

		// handle message
		switch msg := msg.(type) {
		case *apphost.AuthTokenMsg:
			err = guest.onAuthTokenMsg(ctx, msg)
		case *apphost.RouteQueryMsg:
			err = guest.onRouteQueryMsg(ctx, msg)
		case *apphost.RegisterServiceMsg:
			err = guest.onRegisterServiceMsg(ctx, msg)
		case *apphost.AttachQueryMsg:
			err = guest.onAttachQueryMsg(ctx, msg)
		case *apphost.RejectIncomingMsg:
			err = guest.onRejectIncomingMsg(ctx, msg)
		default:
			guest.mod.log.Logv(1, "protocol error: invalid message: %v", msg.ObjectType())
			return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeProtocolError})
		}

		if err != nil {
			return
		}

		if guest.donated.Load() {
			// per-query attach donated the conn to the routing goroutine;
			// stop reading on it from Serve.
			return nil
		}
	}
}

func (guest *Guest) onAuthTokenMsg(ctx *astral.Context, msg *apphost.AuthTokenMsg) (err error) {
	// fetch info about the token from the database
	guest.guestID, err = guest.mod.AuthenticateToken(string(msg.Token))
	if err != nil {
		guest.mod.log.Errorv(3, "token authentication failed")
		return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeAuthFailed})
	}
	guest.mod.log.Infov(3, "%v authenticated using token", guest.guestID)

	return guest.Send(&apphost.AuthSuccessMsg{
		GuestID: guest.guestID,
	})
}

func (guest *Guest) onRegisterHandlerMsg(ctx *astral.Context, msg *apphost.RegisterHandlerMsg) (err error) {
	// only authenticated guests can register handlers
	if !guest.isAuthenticated() {
		return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
	}

	// if requested identity is different from the authenticated identity, check authorization
	if !msg.Identity.IsEqual(guest.guestID) {
		if !guest.mod.Auth.Authorize(ctx, &authmod.SudoAction{Action: auth.NewAction(guest.guestID), AsID: msg.Identity}) {
			return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
		}
	}

	// add the handler
	handler := &IPCHandler{
		Identity: msg.Identity,
		IPCToken: msg.AuthToken,
		Endpoint: string(msg.Endpoint),
	}
	guest.mod.ipcHandlers.Add(handler)
	defer guest.mod.ipcHandlers.Remove(handler) // remove handler on disconnect

	// send ack to the client
	err = guest.Send(&astral.Ack{})
	if err != nil {
		return
	}

	defer guest.Close()
	guest.mod.log.Logv(3, "%v registered a handler for %v at %v", guest.guestID, handler.Identity, handler.Endpoint)

	// NOTE: at this stage this guest connection is only used to keep the query handler alive

	// close connection if context ends
	var done = make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			guest.Close()
		case <-done:
		}
	}()

	// wait for the connection to end ignoring all incoming objects
	for {
		_, err = guest.Receive()
		if err != nil {
			break
		}
	}

	return nil
}

func (guest *Guest) onRouteQueryMsg(ctx *astral.Context, msg *apphost.RouteQueryMsg) (err error) {
	// deny if not authenticated and anonymous queries are not allowed
	if !guest.isAuthenticated() && !guest.mod.config.AllowAnonymous {
		return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
	}

	var q = &astral.Query{
		Nonce:       msg.Nonce,
		Caller:      msg.Caller,
		Target:      msg.Target,
		QueryString: guest.prepareQueryString(string(msg.Query)),
	}

	// An unauthenticated web guest is confined to the anonymous_web_allowlist
	// for the node's claim state.
	if guest.mod.blocksAnonymousWeb(ctx, guest.webOrigin, guest.isAuthenticated(), q) {
		return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
	}

	// check authorization if necessary
	switch {
	case q.Caller.IsZero():
	case q.Caller.IsEqual(guest.guestID):
	default:
		if !guest.mod.Auth.Authorize(ctx, &authmod.SudoAction{Action: auth.NewAction(guest.guestID), AsID: q.Caller}) {
			return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
		}
	}

	// set the zone
	ctx = ctx.WithZone(msg.Zone)

	// set the filters
	if len(msg.Filters) > 0 {
		var filters []string
		for _, name := range msg.Filters {
			filters = append(filters, string(name))
		}
		ctx = ctx.WithFilters(filters...)
	}

	// disable network for guests
	if !guest.isAuthenticated() {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	// prepare query context
	qCtx, cancelQuery := ctx.WithCancelCause()
	defer cancelQuery(nil)

	// keep track of en route queries
	inFlight := astral.Launch(q)

	// Carry the browser Origin for queries arriving over the WebSocket endpoint
	// so ops can apply their own per-origin authorization.
	if guest.webOrigin != "" {
		inFlight.Extra.Set(apphostmod.ExtraOriginWeb, guest.webOrigin)
	}

	// Flag queries from a token-less session - IPC or WebSocket alike - so ops
	// can tell an anonymous caller from the node itself once the core router
	// rewrites a nil caller to the node identity. Set only when true; absence
	// means the session was authenticated.
	if !guest.isAuthenticated() {
		inFlight.Extra.Set(apphostmod.ExtraAnonymous, true)
	}

	enRoute := &queryEnRoute{query: inFlight, cancel: cancelQuery}

	// route the query
	guest.mod.enRoute.Set(q.Nonce, enRoute)
	conn, err := query.RouteInFlight(qCtx, guest.mod.node, inFlight)
	guest.mod.enRoute.Delete(q.Nonce)

	// check error
	var rejected *astral.ErrRejected
	switch {
	case err == nil:
	case errors.As(err, &rejected):
		return guest.Send(&apphost.QueryRejectedMsg{Code: astral.Uint8(rejected.Code)})

	case errors.Is(err, &astral.ErrRouteNotFound{}):
		return guest.sendError(apphost.ErrCodeRouteNotFound)

	case errors.Is(err, context.Canceled):
		return guest.sendError(apphost.ErrCodeCanceled)

	case errors.Is(err, context.DeadlineExceeded):
		return guest.sendError(apphost.ErrCodeTimeout)

	case errors.Is(err, astral.ErrTargetNotAllowed):
		return guest.sendError(apphost.ErrCodeTargetNotAllowed)

	default:
		guest.mod.log.Logv(2, "unexpected error routing query %v: %v", q.Nonce, err)
		return guest.sendError(apphost.ErrCodeInternalError)
	}

	// write success response
	err = guest.Send(&apphost.QueryAcceptedMsg{})
	if err != nil {
		return err
	}

	// proxy the traffic
	_, _, err = streams.Join(guest.conn, conn)

	return
}

func (guest *Guest) sendError(code string) error {
	return guest.Send(&apphost.ErrorMsg{Code: astral.String8(code)})
}

func (guest *Guest) isAuthenticated() bool {
	return !guest.guestID.IsZero()
}

// onRegisterServiceMsg registers this connection as the notification channel for
// inbound queries targeting msg.Identity. Authorization mirrors RouteQueryMsg: caller
// must equal Identity or hold a SudoAction for it.
func (guest *Guest) onRegisterServiceMsg(ctx *astral.Context, msg *apphost.RegisterServiceMsg) error {
	if !guest.isAuthenticated() {
		return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
	}

	if !msg.Identity.IsEqual(guest.guestID) {
		if !guest.mod.Auth.Authorize(ctx, &authmod.SudoAction{Action: auth.NewAction(guest.guestID), AsID: msg.Identity}) {
			return guest.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
		}
	}

	h := &WSHandler{
		Identity: msg.Identity,
		mod:      guest.mod,
		ch:       guest.Channel,
	}
	guest.mod.wsHandlers.Add(h)
	guest.mod.log.Logv(3, "%v registered ws service handler for %v", guest.guestID, msg.Identity)

	// remove on disconnect
	go func() {
		<-ctx.Done()
		guest.mod.wsHandlers.Remove(h)
	}()

	return guest.Send(&astral.Ack{})
}

// onAttachQueryMsg pairs this connection with a pending inbound query that was
// announced earlier via IncomingQueryMsg. On success the conn is "donated" to the
// routing goroutine inside WSHandler.RouteQuery, which then owns its lifecycle.
func (guest *Guest) onAttachQueryMsg(ctx *astral.Context, msg *apphost.AttachQueryMsg) error {
	pending, ok := guest.mod.pendingInboundQueries.Get(msg.QueryID)
	if !ok {
		return guest.sendError(apphost.ErrCodeRouteNotFound)
	}

	if err := guest.Send(&astral.Ack{}); err != nil {
		return err
	}

	// Donate the conn. The routing goroutine in WSHandler.RouteQuery is blocked on
	// pending.attach and will start using the conn as soon as it receives it.
	guest.donated.Store(true)
	select {
	case pending.attach <- guest.conn:
		return nil
	case <-ctx.Done():
		// donation didn't happen — clear the flag so the conn does get closed
		guest.donated.Store(false)
		return ctx.Err()
	}
}

// onRejectIncomingMsg signals to the matching pending inbound query that the registered
// handler refuses this query. The handler's RouteQuery returns ErrRejected with the
// provided code.
func (guest *Guest) onRejectIncomingMsg(ctx *astral.Context, msg *apphost.RejectIncomingMsg) error {
	pending, ok := guest.mod.pendingInboundQueries.Get(msg.QueryID)
	if !ok {
		// nothing to do — caller already moved on
		return nil
	}

	select {
	case pending.reject <- uint8(msg.Code):
	default:
		// channel already has a value (caller raced); drop.
	}
	return nil
}

// prepareQueryString rewrites the query string for JSON-mode guests so the responder
// also speaks JSON. It sets out=json and in=json if not already present. For binary-mode
// guests it returns the string unchanged.
func (guest *Guest) prepareQueryString(s string) string {
	if guest.mode != ModeJSON {
		return s
	}

	path, params := query.Parse(s)
	if _, ok := params["out"]; !ok {
		params["out"] = "json"
	}
	if _, ok := params["in"]; !ok {
		params["in"] = "json"
	}

	encoded, err := query.Marshal(params)
	if err != nil || encoded == "" {
		return path
	}
	return path + "?" + encoded
}
