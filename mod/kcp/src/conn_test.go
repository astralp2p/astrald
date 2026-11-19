package kcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// testDialTimeout is the dial_timeout of the modules these tests build.
const testDialTimeout = 300 * time.Millisecond

// readResult is the outcome of one Read and the time it took.
type readResult struct {
	n       int
	err     error
	elapsed time.Duration
}

// timedRead reads once from conn in the background.
func timedRead(conn interface{ Read([]byte) (int, error) }) <-chan readResult {
	result := make(chan readResult, 1)
	go func() {
		start := time.Now()
		n, err := conn.Read(make([]byte, 64))
		result <- readResult{n: n, err: err, elapsed: time.Since(start)}
	}()

	return result
}

// awaitRead waits for a timedRead result, failing the test after limit.
func awaitRead(t *testing.T, result <-chan readResult, limit time.Duration) readResult {
	t.Helper()

	select {
	case r := <-result:
		return r
	case <-time.After(limit):
		t.Fatalf("read still blocked after %v", limit)
		return readResult{}
	}
}

// dialModule builds a Module that dials with testDialTimeout.
func dialModule() *Module {
	return &Module{
		config:   Config{DialTimeout: testDialTimeout},
		settings: Settings{Dial: &tree.Value[*astral.Bool]{}},
	}
}

// blackHole returns a loopback UDP endpoint that receives every packet and
// never answers, the way a kcp address with nothing behind it looks.
func blackHole(t *testing.T) *kcp.Endpoint {
	t.Helper()

	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen sink: %v", err)
	}
	t.Cleanup(func() { sink.Close() })

	endpoint, err := kcp.ParseEndpoint(sink.LocalAddr().String())
	if err != nil {
		t.Fatalf("parse sink endpoint: %v", err)
	}

	return endpoint
}

// TestDialToDeadAddressFailsWithinDialTimeout pins criterion 3 of the kcp
// re-link ticket: the handshake's first write is queued without any peer, and
// the read of the reply fails once dial_timeout passes.
func TestDialToDeadAddressFailsWithinDialTimeout(t *testing.T) {
	conn, err := dialModule().Dial(astral.NewContext(nil), blackHole(t))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := awaitRead(t, timedRead(conn), 3*time.Second)
	if r.err == nil {
		t.Fatalf("read from a dead address returned %v bytes, want a timeout", r.n)
	}
	if r.elapsed > testDialTimeout+500*time.Millisecond {
		t.Errorf("read failed after %v, want within dial_timeout %v", r.elapsed, testDialTimeout)
	}
}

// TestAnsweredConnOutlivesDialTimeout pins that the first frame from the peer
// ends the dial deadline: an idle live conn reads and writes after dial_timeout.
func TestAnsweredConnOutlivesDialTimeout(t *testing.T) {
	listener, err := kcpgo.ListenWithOptions("127.0.0.1:0", nil, 0, 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	remote, err := kcp.ParseEndpoint(listener.Addr().String())
	if err != nil {
		t.Fatalf("parse listener endpoint: %v", err)
	}

	conn, err := dialModule().Dial(astral.NewContext(nil), remote)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	peer, err := listener.AcceptKCP()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	if _, err := peer.Write([]byte("pong")); err != nil {
		t.Fatalf("peer write pong: %v", err)
	}
	if r := awaitRead(t, timedRead(conn), 3*time.Second); r.err != nil {
		t.Fatalf("read pong: %v", r.err)
	}

	// why: an idle gap longer than dial_timeout would end the conn if the
	// deadline outlived the first answer.
	time.Sleep(2 * testDialTimeout)

	if _, err := conn.Write([]byte("later")); err != nil {
		t.Fatalf("write after dial_timeout: %v", err)
	}
	late := timedRead(conn)
	if _, err := peer.Write([]byte("late")); err != nil {
		t.Fatalf("peer write late: %v", err)
	}
	if r := awaitRead(t, late, 3*time.Second); r.err != nil {
		t.Fatalf("read after dial_timeout: %v", r.err)
	}
}

// TestAcceptedPhantomSessionEndsWithinDialTimeout pins that an accepted conn
// takes the configured dial_timeout. A stray packet from a dead peer's old
// session opens a session that never carries a readable frame.
func TestAcceptedPhantomSessionEndsWithinDialTimeout(t *testing.T) {
	listenPort := reserveUDPPort(t)

	reads := make(chan readResult, 1)
	handler := func(_ context.Context, conn exonetmod.Conn) (bool, error) {
		start := time.Now()
		n, err := conn.Read(make([]byte, 64))
		reads <- readResult{n: n, err: err, elapsed: time.Since(start)}
		return false, nil
	}

	mod := &Module{log: log.New(astral.GenerateIdentity()), config: Config{DialTimeout: testDialTimeout}}
	srv := NewServer(mod, astral.Uint16(listenPort), handler, 0)
	startRun(t, srv)
	t.Cleanup(func() { srv.Close() })

	// why: a packet sent before the listener binds draws an ICMP port
	// unreachable, which a connected socket reports as ECONNREFUSED on its next
	// write; an unconnected socket drops it, so the retry below can proceed.
	stray, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("open stray socket: %v", err)
	}
	defer stray.Close()
	listenAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: listenPort}

	// note: kcp-go accepts a session for any packet of at least the 24-byte
	// kcp header from a new address; a zeroed header carries no frame.
	// why: the listener may bind after the first packet, so the packet repeats
	// until a session is accepted.
	deadline := time.After(3 * time.Second)
	for {
		if _, err := stray.WriteToUDP(make([]byte, kcpgo.IKCP_OVERHEAD), listenAddr); err != nil {
			t.Fatalf("send stray packet: %v", err)
		}

		select {
		case r := <-reads:
			if r.err == nil {
				t.Fatalf("phantom session read %v bytes, want a timeout", r.n)
			}
			if r.elapsed > testDialTimeout+500*time.Millisecond {
				t.Errorf("phantom session ended after %v, want within dial_timeout %v", r.elapsed, testDialTimeout)
			}
			return
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatal("no phantom session ended within 3 s")
		}
	}
}
