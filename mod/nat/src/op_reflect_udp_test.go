package nat

import (
	"bytes"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
)

// reflectConn is the reflector's socket in a test. It waits for the offer the
// op wrote to the caller, then delivers the scripted datagrams; a nil payload
// stands for the offer's token. It records writes and its close.
type reflectConn struct {
	caller *recordingWriter
	script []packet

	mu       sync.Mutex
	deadline time.Time
	writes   int
	closedAt time.Time
}

func (c *reflectConn) ReadFrom(b []byte) (int, net.Addr, error) {
	c.mu.Lock()
	deadline := c.deadline
	c.mu.Unlock()

	for time.Now().Before(deadline) {
		offer := c.offer()
		c.mu.Lock()
		if offer != nil && len(c.script) > 0 {
			p := c.script[0]
			c.script = c.script[1:]
			c.mu.Unlock()
			data := p.data
			if data == nil {
				data = offer.Token
			}
			return copy(b, data), p.from, nil
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return 0, nil, os.ErrDeadlineExceeded
}

// offer is the nat.reflect_udp_offer the op has sent so far, or nil.
func (c *reflectConn) offer() *nat.ReflectUDPOffer {
	c.caller.mu.Lock()
	defer c.caller.mu.Unlock()
	o, err := channel.NewReceiver(bytes.NewReader(c.caller.buf.Bytes())).Receive()
	if err != nil {
		return nil
	}
	offer, _ := o.(*nat.ReflectUDPOffer)
	return offer
}

func (c *reflectConn) WriteTo([]byte, net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes++
	return 0, nil
}

func (c *reflectConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closedAt.IsZero() {
		c.closedAt = time.Now()
	}
	return nil
}

func (c *reflectConn) LocalAddr() net.Addr                { return &net.UDPAddr{IP: net.IPv4zero, Port: 47001} }
func (c *reflectConn) SetDeadline(t time.Time) error      { return c.SetReadDeadline(t) }
func (c *reflectConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *reflectConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	return nil
}

// runReflect routes nat.reflect_udp to an authorized, enabled module whose
// socket is conn, and waits for the op to close the caller's connection.
func runReflect(t *testing.T, conn *reflectConn) (objs []astral.Object, took time.Duration) {
	t.Helper()

	mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}, log: log.New(nil)}
	mod.enabled.Store(true)
	mod.listenUDP = func() (net.PacketConn, error) { return conn, nil }

	start := time.Now()
	if err := route(t, mod.OpReflectUDP, astral.GenerateIdentity(), nat.MethodReflectUDP, conn.caller); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-conn.caller.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}
	return conn.caller.objects(t), time.Since(start)
}

func udp(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		t.Fatalf("resolve %q: %v", s, err)
	}
	return a
}

// The reflector answers with exactly the source of the datagram that carries
// its token, over the channel, and never writes UDP.
func TestOpReflectUDP_AnswersTheSourceOfTheToken(t *testing.T) {
	conn := &reflectConn{
		caller: newRecordingWriter(),
		script: []packet{
			{data: bytes.Repeat([]byte{0x01}, reflectTokenSize), from: udp(t, "198.51.100.1:9")},
			{data: nil, from: udp(t, "203.0.113.9:6635")},
		},
	}

	objs, took := runReflect(t, conn)

	if len(objs) != 2 {
		t.Fatalf("answered %d objects; want an offer and an endpoint: %v", len(objs), objs)
	}
	offer, ok := objs[0].(*nat.ReflectUDPOffer)
	if !ok || offer.Port != 47001 || len(offer.Token) != reflectTokenSize {
		t.Fatalf("first answer %#v; want an offer for port 47001 with a %d-byte token", objs[0], reflectTokenSize)
	}
	seen, ok := objs[1].(*nat.Endpoint)
	if !ok || seen.String() != "203.0.113.9:6635" {
		t.Fatalf("second answer %v; want the endpoint 203.0.113.9:6635", objs[1])
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.writes != 0 {
		t.Errorf("wrote %d UDP datagrams; want none", conn.writes)
	}
	if conn.closedAt.IsZero() || took > nat.ReflectTimeout {
		t.Errorf("socket closed=%v after %v; want it closed within %v", !conn.closedAt.IsZero(), took, nat.ReflectTimeout)
	}
}

// Without the token the reflector gives up after nat.ReflectTimeout, closes its
// socket, and answers ErrNoReflection.
func TestOpReflectUDP_GivesUpAfterTheTimeout(t *testing.T) {
	conn := &reflectConn{caller: newRecordingWriter()}

	objs, took := runReflect(t, conn)

	if len(objs) != 2 {
		t.Fatalf("answered %d objects; want an offer and an error: %v", len(objs), objs)
	}
	msg, ok := objs[1].(*astral.ErrorMessage)
	if !ok || msg.Error() != nat.ErrNoReflection.Error() {
		t.Fatalf("second answer %v; want %q", objs[1], nat.ErrNoReflection)
	}
	if took < nat.ReflectTimeout-100*time.Millisecond || took > nat.ReflectTimeout+time.Second {
		t.Errorf("gave up after %v; want about %v", took, nat.ReflectTimeout)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closedAt.IsZero() || conn.writes != 0 {
		t.Errorf("socket closed=%v writes=%d; want closed and no writes", !conn.closedAt.IsZero(), conn.writes)
	}
}

// A node that does not traverse NAT refuses to reflect and binds no socket.
func TestOpReflectUDP_RefusesWhenDisabled(t *testing.T) {
	bound := false
	mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}, log: log.New(nil)}
	mod.listenUDP = func() (net.PacketConn, error) { bound = true; return nil, os.ErrInvalid }
	w := newRecordingWriter()

	if err := route(t, mod.OpReflectUDP, astral.GenerateIdentity(), nat.MethodReflectUDP, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := w.refusal(t); got != nat.ErrDisabled.Error() {
		t.Fatalf("answered %q; want %q", got, nat.ErrDisabled)
	}
	if bound {
		t.Fatal("bound a socket on a node that does not traverse NAT")
	}
}
