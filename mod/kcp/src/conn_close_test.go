package kcp

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/astral"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// answeredPair dials a kcp listener on loopback with dialModule and returns
// the dialed conn and the accepted session once each end has read from the other.
func answeredPair(t *testing.T) (*WrappedConn, *kcpgo.UDPSession) {
	t.Helper()
	listener, err := kcpgo.ListenWithOptions("127.0.0.1:0", nil, 0, 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	remote, err := kcp.ParseEndpoint(listener.Addr().String())
	if err != nil {
		t.Fatalf("parse listener endpoint: %v", err)
	}
	conn, err := dialModule().Dial(astral.NewContext(nil), remote)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	peer, err := listener.AcceptKCP()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	t.Cleanup(func() { peer.Close() })
	if _, err := peer.Write([]byte("pong")); err != nil {
		t.Fatalf("peer write pong: %v", err)
	}
	if r := awaitRead(t, timedRead(conn), 3*time.Second); r.err != nil {
		t.Fatalf("read pong: %v", r.err)
	}
	return conn.(*WrappedConn), peer
}

// TestCloseEndsReadsAndWritesAtOnce pins that Close of an answered conn
// releases a blocked Read at once and fails later calls with io.ErrClosedPipe,
// although its session lingers.
func TestCloseEndsReadsAndWritesAtOnce(t *testing.T) {
	conn, _ := answeredPair(t)
	blocked := timedRead(conn)
	time.Sleep(50 * time.Millisecond)

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if r := awaitRead(t, blocked, 200*time.Millisecond); !errors.Is(r.err, io.ErrClosedPipe) {
		t.Fatalf("blocked Read ended with %v; want %v", r.err, io.ErrClosedPipe)
	}
	if _, err := conn.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after Close: %v; want %v", err, io.ErrClosedPipe)
	}
	if err := conn.Close(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("second Close: %v; want %v", err, io.ErrClosedPipe)
	}
}

// TestCloseDeliversQueuedWrites pins that data written right before Close
// reaches the peer: the session lingers past Close until the peer acknowledges it.
func TestCloseDeliversQueuedWrites(t *testing.T) {
	conn, peer := answeredPair(t)
	const chunks = 8
	for i := 0; i < chunks; i++ {
		if _, err := conn.Write([]byte("data")); err != nil {
			t.Fatalf("write %v: %v", i, err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_ = peer.SetReadDeadline(time.Now().Add(closeLinger))
	got := make([]byte, 0, 4*chunks)
	buf := make([]byte, 64)
	for len(got) < cap(got) {
		n, err := peer.Read(buf)
		if err != nil {
			t.Fatalf("peer read %v of %v bytes written before Close: %v", len(got), cap(got), err)
		}
		got = append(got, buf[:n]...)
	}
}

// TestCloseOfUnansweredConnReleasesPortAtOnce pins that a dialed conn whose
// peer never answered closes its UDP socket in Close, with no linger.
func TestCloseOfUnansweredConnReleasesPortAtOnce(t *testing.T) {
	conn, err := dialModule().Dial(astral.NewContext(nil), blackHole(t))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	addr := conn.(*WrappedConn).udpConn.LocalAddr().(*net.UDPAddr)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rebind, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("local port %v still bound after Close: %v", addr.Port, err)
	}
	rebind.Close()
}
