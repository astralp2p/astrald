package nat

import (
	"bytes"
	"context"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
)

func TestCandidatePortsBounds(t *testing.T) {
	tests := []struct {
		center, spread int
		first, last    int
	}{
		{1000, 10, 990, 1010},
		{1, 10, 1, 11},
		{65535, 10, 65525, 65535},
		{65530, 10, 65520, 65535},
	}
	for _, tt := range tests {
		ports := candidatePorts(tt.center, tt.spread)
		want := tt.last - tt.first + 1
		if len(ports) != want || ports[0] != tt.first || ports[len(ports)-1] != tt.last {
			t.Errorf("candidatePorts(%d,%d) = len %d [%d..%d], want len %d [%d..%d]",
				tt.center, tt.spread, len(ports), ports[0], ports[len(ports)-1], want, tt.first, tt.last)
		}
	}
}

func portRange(from, to int) (ports []int) {
	for p := from; p <= to; p++ {
		ports = append(ports, p)
	}
	return
}

func TestCandidatePorts(t *testing.T) {
	tests := []struct {
		name           string
		center, spread int
		want           []int
	}{
		{"window around center", 1000, 10, portRange(990, 1010)},
		{"zero spread", 1000, 0, []int{1000}},
		{"negative spread", 1000, -3, []int{1000}},
		{"clamped at min port", 3, 10, portRange(1, 13)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidatePorts(tt.center, tt.spread); !slices.Equal(got, tt.want) {
				t.Errorf("candidatePorts(%d, %d) = %v; want %v", tt.center, tt.spread, got, tt.want)
			}
		})
	}
}

func TestPreparePunchTargets(t *testing.T) {
	peer := ip.IP(net.ParseIP("203.0.113.5"))

	addrs, err := preparePunchTargets(peer, 1791)
	if err != nil {
		t.Fatalf("preparePunchTargets: %v", err)
	}
	if len(addrs) != 2*portGuessRange+1 {
		t.Errorf("got %d targets; want %d", len(addrs), 2*portGuessRange+1)
	}

	for _, a := range addrs {
		if !a.IP.Equal(net.IP(peer)) {
			t.Errorf("target %v; want host %v", a, peer)
		}
	}

	if first, last := addrs[0].Port, addrs[len(addrs)-1].Port; first != 1781 || last != 1801 {
		t.Errorf("targets span %d..%d; want 1781..1801", first, last)
	}
}

func TestJitterBounds(t *testing.T) {
	const base = 25 * time.Millisecond

	if got := jitter(base, 0); got != base {
		t.Errorf("jitter(%v, 0) = %v; want %v", base, got, base)
	}

	for i := 0; i < 1000; i++ {
		got := jitter(base, 3*time.Millisecond)
		if got < 22*time.Millisecond || got > 28*time.Millisecond {
			t.Fatalf("jitter(%v, 3ms) = %v; want within [22ms, 28ms]", base, got)
		}
	}
}

func TestNewConePuncherSession(t *testing.T) {
	p, err := newConePuncher(nil, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher(nil): %v", err)
	}
	if n := len(p.Session()); n != 16 {
		t.Errorf("generated session is %d bytes; want 16", n)
	}

	_, err = newConePuncher(make([]byte, 8), &ConePuncherCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "session must be 16 bytes") {
		t.Errorf("newConePuncher(8 bytes) error = %v; want \"session must be 16 bytes\"", err)
	}
}

func TestConePuncherSessionIsCopied(t *testing.T) {
	session := bytes.Repeat([]byte{0xab}, 16)
	original := bytes.Clone(session)

	p, err := newConePuncher(session, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}

	session[0] = 0
	p.Session()[0] = 0

	if got := p.Session(); !bytes.Equal(got, original) {
		t.Errorf("Session() = % x; want % x", got, original)
	}
}

func TestConePuncherHolePunchValidatesArgs(t *testing.T) {
	p, err := newConePuncher(nil, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	peer := ip.IP(net.ParseIP("203.0.113.5"))

	tests := []struct {
		name    string
		peer    ip.IP
		port    int
		wantErr string
	}{
		{"nil peer", nil, 1791, "empty peer IP"},
		{"port zero", peer, 0, "invalid peer port"},
		{"port above range", peer, 65536, "invalid peer port"},
		{"not opened", peer, 1791, "no UDP connection available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := p.HolePunch(ctx, tt.peer, tt.port)
			if res != nil {
				t.Errorf("HolePunch result = %+v; want nil", res)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("HolePunch(%v, %d) error = %v; want %q", tt.peer, tt.port, err, tt.wantErr)
			}
		})
	}
}

// scriptedConn is a net.PacketConn that delivers a fixed list of datagrams,
// then times out every read, and records every write.
type scriptedConn struct {
	mu       sync.Mutex
	inbox    []packet
	writes   []packet
	deadline time.Time
	closed   bool
}

func (c *scriptedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, nil, net.ErrClosed
	}
	if len(c.inbox) > 0 {
		p := c.inbox[0]
		c.inbox = c.inbox[1:]
		c.mu.Unlock()
		return copy(b, p.data), p.from, nil
	}
	wait := time.Until(c.deadline)
	c.mu.Unlock()

	time.Sleep(max(wait, time.Millisecond))
	return 0, nil, os.ErrDeadlineExceeded
}

func (c *scriptedConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, packet{data: bytes.Clone(b), from: addr})
	return len(b), nil
}

func (c *scriptedConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *scriptedConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4zero, Port: 38826}
}

func (c *scriptedConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *scriptedConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	return nil
}

func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

// writesTo counts the datagrams written to addr, and how many carried session.
func (c *scriptedConn) writesTo(addr string, session []byte) (all, withSession int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.writes {
		if w.from.String() != addr {
			continue
		}
		all++
		if bytes.Equal(w.data, session) {
			withSession++
		}
	}
	return
}

func udpAddr(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		t.Fatalf("resolve %q: %v", s, err)
	}
	return a
}

// openScripted returns a puncher whose socket is conn, as Open would leave it.
func openScripted(t *testing.T, conn *scriptedConn) *conePuncher {
	t.Helper()
	p, err := newConePuncher(nil, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}
	cp := p
	cp.conn, cp.localPort = conn, 38826
	return cp
}

// A NAT that translates ports delivers the peer's probe from a port nobody
// announced: the peer announced 38826 and its probe arrives from 6635.
func TestHolePunch_AcceptsTheSessionFromAnyPortOfThePeer(t *testing.T) {
	conn := &scriptedConn{}
	p := openScripted(t, conn)
	conn.inbox = []packet{{data: p.Session(), from: udpAddr(t, "203.0.113.9:6635")}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := p.HolePunch(ctx, ip.IP(net.ParseIP("203.0.113.9")), 38826)
	if err != nil {
		t.Fatalf("HolePunch: %v", err)
	}
	if res.RemotePort != 6635 || !net.IP(res.RemoteIP).Equal(net.ParseIP("203.0.113.9")) {
		t.Errorf("result remote = %v:%d; want 203.0.113.9:6635", res.RemoteIP, res.RemotePort)
	}
	if res.LocalPort != 38826 {
		t.Errorf("result local port = %d; want 38826", res.LocalPort)
	}

	all, withSession := conn.writesTo("203.0.113.9:6635", p.Session())
	if all != packetsPerBurst || withSession != packetsPerBurst {
		t.Errorf("wrote %d datagrams (%d with the session) to 203.0.113.9:6635; want %d session datagrams",
			all, withSession, packetsPerBurst)
	}
}

// The session from another IP, or a wrong session from the peer's IP, is not a
// probe; after the first accepted probe nothing more is read or answered.
func TestHolePunch_IgnoresForeignProbesAndAnswersOnce(t *testing.T) {
	conn := &scriptedConn{}
	p := openScripted(t, conn)
	wrong := bytes.Repeat([]byte{0x5a}, 16)
	conn.inbox = []packet{
		{data: p.Session(), from: udpAddr(t, "198.51.100.77:6635")},
		{data: wrong, from: udpAddr(t, "203.0.113.9:6000")},
		{data: p.Session()[:8], from: udpAddr(t, "203.0.113.9:6001")},
		{data: p.Session(), from: udpAddr(t, "203.0.113.9:6635")},
		{data: p.Session(), from: udpAddr(t, "203.0.113.9:7000")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := p.HolePunch(ctx, ip.IP(net.ParseIP("203.0.113.9")), 38826)
	if err != nil {
		t.Fatalf("HolePunch: %v", err)
	}
	if res.RemotePort != 6635 {
		t.Errorf("result remote port = %d; want 6635", res.RemotePort)
	}

	// why: give a stray second answer the time to show up before counting.
	time.Sleep(3 * burstInterval)

	for addr, want := range map[string]int{
		"198.51.100.77:6635": 0,
		"203.0.113.9:6000":   0,
		"203.0.113.9:6001":   0,
		"203.0.113.9:6635":   packetsPerBurst,
		"203.0.113.9:7000":   0,
	} {
		if got, _ := conn.writesTo(addr, p.Session()); got != want {
			t.Errorf("wrote %d datagrams to %s; want %d", got, addr, want)
		}
	}

	conn.mu.Lock()
	left := len(conn.inbox)
	conn.mu.Unlock()
	if left != 1 {
		t.Errorf("%d datagrams left unread; want 1 (the probe after the accepted one)", left)
	}
}
