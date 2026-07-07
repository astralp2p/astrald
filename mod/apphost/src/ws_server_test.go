package apphost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
)

// minimalHTTPServer returns an HTTPServer whose handleWS can negotiate but whose
// Guest.Serve will panic on first welcome write (Dir is nil). Tests that only exercise
// the negotiation must close the WS client-side before Serve sends HostInfoMsg, OR
// must call srv.negotiateWS directly.
func minimalHTTPServer(t *testing.T) *HTTPServer {
	t.Helper()
	mod := &Module{
		config: Config{},
		log:    log.New(nil),
	}
	return &HTTPServer{
		Module: mod,
		ctx:    astral.NewContext(nil),
	}
}

func dialWS(t *testing.T, srv *httptest.Server, subproto string) (*websocket.Conn, *http.Response) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/.ws"
	opts := &websocket.DialOptions{}
	if subproto != "" {
		opts.Subprotocols = []string{subproto}
	}

	c, resp, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		// caller may want to inspect resp.StatusCode even on dial error
		return nil, resp
	}
	return c, resp
}

type negotiationResult struct {
	mode Mode
	fmt  string
	ok   bool
}

func negotiationCaptureServer(t *testing.T, srv *HTTPServer) (*httptest.Server, <-chan negotiationResult) {
	t.Helper()
	ch := make(chan negotiationResult, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, mode, chFmt := srv.negotiateWS(w, r)
		if conn == nil {
			ch <- negotiationResult{ok: false}
			return
		}
		ch <- negotiationResult{mode: mode, fmt: chFmt, ok: true}
		conn.Close()
	}))
	return ts, ch
}

func TestWS_NegotiationSelectsBinary(t *testing.T) {
	ts, captured := negotiationCaptureServer(t, minimalHTTPServer(t))
	defer ts.Close()

	c, _ := dialWS(t, ts, SubprotocolBinary)
	if c == nil {
		t.Fatal("dial failed")
	}
	defer c.CloseNow()

	if got := c.Subprotocol(); got != SubprotocolBinary {
		t.Errorf("client sees subprotocol %q, want %q", got, SubprotocolBinary)
	}

	select {
	case r := <-captured:
		if !r.ok {
			t.Fatal("server did not capture a negotiation")
		}
		if r.mode != ModeBinary {
			t.Errorf("server captured mode = %v, want ModeBinary", r.mode)
		}
		if r.fmt != channel.Binary {
			t.Errorf("server captured chFmt = %q, want %q", r.fmt, channel.Binary)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not complete")
	}
}

func TestWS_NegotiationSelectsJSON(t *testing.T) {
	ts, captured := negotiationCaptureServer(t, minimalHTTPServer(t))
	defer ts.Close()

	c, _ := dialWS(t, ts, SubprotocolJSON)
	if c == nil {
		t.Fatal("dial failed")
	}
	defer c.CloseNow()

	if got := c.Subprotocol(); got != SubprotocolJSON {
		t.Errorf("client sees subprotocol %q, want %q", got, SubprotocolJSON)
	}

	select {
	case r := <-captured:
		if !r.ok {
			t.Fatal("server did not capture a negotiation")
		}
		if r.mode != ModeJSON {
			t.Errorf("server captured mode = %v, want ModeJSON", r.mode)
		}
		if r.fmt != channel.JSON {
			t.Errorf("server captured chFmt = %q, want %q", r.fmt, channel.JSON)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not complete")
	}
}

func TestWS_LoopbackCrossPortOriginAllowed(t *testing.T) {
	// A browser tab served from 127.0.0.1:8627 connecting to apphost on 127.0.0.1:8624
	// must be allowed by default — DNS rebinding can't forge a loopback Origin, so
	// this is safe and is the common case (e.g. cmd/ws-client).
	ts, captured := negotiationCaptureServer(t, minimalHTTPServer(t))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/.ws"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolJSON},
		HTTPHeader:   http.Header{"Origin": []string{"http://127.0.0.1:18627"}},
	})
	if err != nil {
		t.Fatalf("dial with cross-port loopback origin should succeed: %v", err)
	}
	defer c.CloseNow()

	select {
	case r := <-captured:
		if !r.ok {
			t.Fatal("negotiation rejected a cross-port loopback origin")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not complete")
	}
}

func TestWS_NoSubprotocol_PolicyViolation(t *testing.T) {
	srv := minimalHTTPServer(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := srv.negotiateWS(w, r)
		if conn != nil {
			t.Error("negotiateWS returned a conn without subprotocol")
			conn.Close()
		}
	}))
	defer ts.Close()

	c, _ := dialWS(t, ts, "")
	if c == nil {
		t.Fatal("dial failed")
	}
	defer c.CloseNow()

	// The server should close with StatusPolicyViolation.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("expected close error from server, got nil")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v, want StatusPolicyViolation", status)
	}
}

func TestWS_NegotiationWithRealHandler_PostAcceptIO(t *testing.T) {
	srv := minimalHTTPServer(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := srv.negotiateWS(w, r)
		if conn == nil {
			return
		}
		defer conn.Close()

		// Echo whatever bytes the client sends. Confirms the WS-as-stream pipeline is
		// usable end-to-end for both modes without involving any apphost Module logic.
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}))
	defer ts.Close()

	cases := []struct {
		name     string
		subproto string
		msgType  websocket.MessageType
	}{
		{"binary", SubprotocolBinary, websocket.MessageBinary},
		{"json", SubprotocolJSON, websocket.MessageText},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/.ws"
			wc, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
				Subprotocols: []string{c.subproto},
			})
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer wc.CloseNow()

			nc := websocket.NetConn(ctx, wc, c.msgType)
			// newline-terminated: text mode only flushes complete lines
			payload := []byte("hello-astral\n")
			if _, err := nc.Write(payload); err != nil {
				t.Fatalf("write: %v", err)
			}
			echo := make([]byte, len(payload))
			if _, err := nc.Read(echo); err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(echo) != string(payload) {
				t.Errorf("echo = %q, want %q", echo, payload)
			}
			nc.Close()
		})
	}
}
