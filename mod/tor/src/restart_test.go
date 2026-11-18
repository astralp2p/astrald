package tor

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// fakeTor serves a control socket that asks for no authentication and answers ADD_ONION with
// testServiceID. stop closes the socket and every control connection, as a Tor restart does.
type fakeTor struct {
	l     net.Listener
	added chan string

	mu    sync.Mutex
	conns []net.Conn
}

func startFakeTor(t *testing.T, path string) *fakeTor {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %v: %v", path, err)
	}
	ft := &fakeTor{l: l, added: make(chan string, 16)}
	t.Cleanup(ft.stop)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			ft.mu.Lock()
			ft.conns = append(ft.conns, conn)
			ft.mu.Unlock()
			go ft.serve(conn)
		}
	}()
	return ft
}

func (ft *fakeTor) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		reply := "250 OK\r\n"
		switch {
		case strings.HasPrefix(line, "PROTOCOLINFO"):
			reply = "250-PROTOCOLINFO 1\r\n250-AUTH METHODS=NULL\r\n250-VERSION Tor=\"0.4.8.13\"\r\n250 OK\r\n"
		case strings.HasPrefix(line, "ADD_ONION"):
			ft.added <- line
			reply = "250-ServiceID=" + testServiceID + "\r\n250 OK\r\n"
		}
		if _, err = conn.Write([]byte(reply)); err != nil {
			return
		}
	}
}

func (ft *fakeTor) stop() {
	ft.l.Close()
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for _, c := range ft.conns {
		c.Close()
	}
}

// logLines collects the text of every entry a logger emits.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (c *logLines) LogEntry(e *log.Entry) {
	var parts []string
	for _, o := range e.Objects {
		parts = append(parts, astral.Stringify(o))
	}
	c.mu.Lock()
	c.lines = append(c.lines, strings.Join(parts, ""))
	c.mu.Unlock()
}

func (c *logLines) contains(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.lines {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// testNode is the local node ResolveEndpoints answers for.
type testNode struct {
	astral.Router
	id *astral.Identity
}

func (n testNode) Identity() *astral.Identity { return n.id }

type onionHarness struct {
	mod         *Module
	controlPath string
	addOnion    string // the ADD_ONION line the stored key produces
	log         *logLines
}

// newOnionHarness loads a module whose control socket and onion target are unix sockets in a
// temporary directory, with a stored tor.key and a short retry backoff.
func newOnionHarness(t *testing.T) *onionHarness {
	t.Helper()
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "control")
	targetPath := filepath.Join(dir, "onion.sock")

	mod := loadModule(t, "control_addr: unix:"+controlPath+"\nonion_target: unix:"+targetPath+"\n")

	key := make([]byte, 64)
	rand.Read(key)
	mod.assets = &keyAssets{data: key}
	mod.node = testNode{id: astral.GenerateIdentity()}
	lines := &logLines{}
	mod.log = &log.Logger{}
	mod.log.AddLogger(lines)

	oldMin, oldMax := retryMin, retryMax
	retryMin, retryMax = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { retryMin, retryMax = oldMin, oldMax })

	return &onionHarness{
		mod:         mod,
		controlPath: controlPath,
		addOnion:    "ADD_ONION ED25519-V3:" + base64.StdEncoding.EncodeToString(key) + " Port=1791,unix:" + targetPath,
		log:         lines,
	}
}

// run starts the onion service as the listen setting does, and stops it when the test ends.
func (h *onionHarness) run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.mod.startServer(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("startServer did not return after its context ended")
		}
	})
}

// advertised returns the endpoints ResolveEndpoints gives for the local node.
func (h *onionHarness) advertised(t *testing.T) []string {
	t.Helper()
	ch, err := h.mod.ResolveEndpoints(astral.NewContext(nil), h.mod.node.Identity())
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}
	var out []string
	for e := range ch {
		out = append(out, e.Endpoint.Address())
	}
	return out
}

func expectAddOnion(t *testing.T, ft *fakeTor, want string) {
	t.Helper()
	select {
	case line := <-ft.added:
		if line != want {
			t.Fatalf("ADD_ONION line = %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the onion service was not registered")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestOnionServiceReturnsAfterTorRestart: when Tor restarts, the node stops advertising its onion
// endpoint and logs the loss, then registers the service again with the same key once Tor is back.
func TestOnionServiceReturnsAfterTorRestart(t *testing.T) {
	h := newOnionHarness(t)
	tor1 := startFakeTor(t, h.controlPath)
	h.run(t)

	expectAddOnion(t, tor1, h.addOnion)
	want := testServiceID + ".onion:1791"
	eventually(t, "the onion endpoint is advertised", func() bool {
		eps := h.advertised(t)
		return len(eps) == 1 && eps[0] == want
	})

	tor1.stop()

	eventually(t, "the onion endpoint is withdrawn", func() bool { return len(h.advertised(t)) == 0 })
	eventually(t, "the loss is logged", func() bool {
		return h.log.contains("onion service tor:" + want + " is down: tor control connection closed")
	})

	tor2 := startFakeTor(t, h.controlPath)

	expectAddOnion(t, tor2, h.addOnion)
	eventually(t, "the onion endpoint is advertised again", func() bool {
		eps := h.advertised(t)
		return len(eps) == 1 && eps[0] == want
	})
}

// TestOnionServiceStartsWhenTorAppears: with no control socket at startup, the node retries and
// registers the service once Tor starts.
func TestOnionServiceStartsWhenTorAppears(t *testing.T) {
	h := newOnionHarness(t)
	h.run(t)

	// note: several retries at the test backoff happen before Tor appears
	time.Sleep(100 * time.Millisecond)
	if eps := h.advertised(t); len(eps) != 0 {
		t.Fatalf("advertised %v with no onion service", eps)
	}

	ft := startFakeTor(t, h.controlPath)

	expectAddOnion(t, ft, h.addOnion)
	eventually(t, "the onion endpoint is advertised", func() bool { return len(h.advertised(t)) == 1 })
}
