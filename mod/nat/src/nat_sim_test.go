package nat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// simNet is an in-memory IPv4 UDP network. A host is public, or private
// behind a simNAT; a datagram reaches a socket only through the NAT rules.
type simNet struct {
	mu      sync.Mutex
	socks   map[string]*simConn // bound sockets by host address and port
	nats    map[string]*simNAT  // NATs by public IP
	dropped int                 // datagrams no socket received
}

// simNAT is an endpoint-independent mapping NAT with port-restricted filtering:
// one external port per local socket, whatever the destination, and inbound
// datagrams only from an address the socket has sent to.
type simNAT struct {
	public  net.IP
	ports   map[int]int             // local port -> external port; a missing port is preserved
	out     map[string]int          // local socket -> external port
	in      map[int]string          // external port -> local socket
	allowed map[int]map[string]bool // external port -> remote addresses sent to
}

func newSimNet() *simNet {
	return &simNet{socks: map[string]*simConn{}, nats: map[string]*simNAT{}}
}

// addNAT adds a NAT at public that maps the local ports in ports; nil preserves every port.
func (n *simNet) addNAT(public string, ports map[int]int) *simNAT {
	nt := &simNAT{
		public:  net.ParseIP(public).To4(),
		ports:   ports,
		out:     map[string]int{},
		in:      map[int]string{},
		allowed: map[int]map[string]bool{},
	}
	n.nats[nt.public.String()] = nt
	return nt
}

// bind opens a socket at host:port; nt is the host's NAT, nil for a public host.
func (n *simNet) bind(t *testing.T, host string, port int, nt *simNAT) *simConn {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	c := &simConn{
		net:   n,
		nat:   nt,
		local: &net.UDPAddr{IP: net.ParseIP(host).To4(), Port: port},
		wake:  make(chan struct{}, 1),
	}
	if _, taken := n.socks[c.local.String()]; taken {
		t.Fatalf("%v is already bound", c.local)
	}
	n.socks[c.local.String()] = c
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// outbound maps a socket for a datagram to dst and returns its external source.
func (nt *simNAT) outbound(local, dst *net.UDPAddr) *net.UDPAddr {
	ext, ok := nt.out[local.String()]
	if !ok {
		ext = local.Port
		if p, mapped := nt.ports[local.Port]; mapped {
			ext = p
		}
		nt.out[local.String()] = ext
		nt.in[ext] = local.String()
		nt.allowed[ext] = map[string]bool{}
	}
	nt.allowed[ext][dst.String()] = true
	return &net.UDPAddr{IP: nt.public, Port: ext}
}

// inbound returns the local socket a datagram from src to external port ext
// reaches, or "" when the NAT drops it.
func (nt *simNAT) inbound(src *net.UDPAddr, ext int) string {
	local, ok := nt.in[ext]
	if !ok || !nt.allowed[ext][src.String()] {
		return ""
	}
	return local
}

func (n *simNet) send(from *simConn, b []byte, dst *net.UDPAddr) {
	n.mu.Lock()
	defer n.mu.Unlock()

	src := from.local
	if from.nat != nil {
		src = from.nat.outbound(from.local, dst)
	}

	key := dst.String()
	if nt, ok := n.nats[dst.IP.To4().String()]; ok {
		key = nt.inbound(src, dst.Port)
	}
	to, ok := n.socks[key]
	if !ok {
		n.dropped++
		return
	}
	to.push(packet{data: bytes.Clone(b), from: src})
}

func (n *simNet) unbind(c *simConn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.socks[c.local.String()] == c {
		delete(n.socks, c.local.String())
	}
}

// simConn is a socket on a simNet host.
type simConn struct {
	net   *simNet
	nat   *simNAT
	local *net.UDPAddr
	wake  chan struct{}

	mu       sync.Mutex
	inbox    []packet
	deadline time.Time
	closed   bool
}

func (c *simConn) push(p packet) {
	c.mu.Lock()
	c.inbox = append(c.inbox, p)
	c.mu.Unlock()
	c.signal()
}

func (c *simConn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *simConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
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
		deadline := c.deadline
		c.mu.Unlock()

		if deadline.IsZero() {
			<-c.wake
			continue
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, nil, os.ErrDeadlineExceeded
		}
		timer := time.NewTimer(wait)
		select {
		case <-c.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (c *simConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	dst, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("not a UDP address: %v", addr)
	}
	c.net.send(c, b, dst)
	return len(b), nil
}

func (c *simConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.net.unbind(c)
	c.signal()
	return nil
}

func (c *simConn) LocalAddr() net.Addr                { return c.local }
func (c *simConn) SetDeadline(t time.Time) error      { return c.SetReadDeadline(t) }
func (c *simConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *simConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	c.signal()
	return nil
}

// simNode is one punch side: its identity, the IP it advertises without a
// reflection, and its host on the simNet.
type simNode struct {
	id        *astral.Identity
	advertise string
	host      string
	nat       *simNAT
	port      int      // local port of the punch socket
	reflector *simConn // public socket the node reflects its punch socket off; nil: none
	net       *simNet
}

// simReflector runs the reflector side of nat.reflect_udp (reflectOnce) on a
// public simNet socket, and sends the offered token from the punch socket.
type simReflector struct {
	conn    *simConn
	puncher *conePuncher
}

func (r *simReflector) Reflect(ctx *astral.Context) (*nat.Endpoint, error) {
	offers := &offerRelay{send: func(o *nat.ReflectUDPOffer) {
		to := &net.UDPAddr{IP: r.conn.local.IP, Port: int(o.Port)}
		_ = r.puncher.sendFromSocket(o.Token, to, reflectTries)
	}}
	return reflectOnce(r.conn, channel.New(offers))
}

// offerRelay is the query channel of a reflection: it hands the offer to send
// once it is complete.
type offerRelay struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	sent bool
	send func(*nat.ReflectUDPOffer)
}

func (o *offerRelay) Read([]byte) (int, error) { return 0, io.EOF }

func (o *offerRelay) Write(b []byte) (int, error) {
	o.mu.Lock()
	o.buf.Write(b)
	obj, err := channel.NewReceiver(bytes.NewReader(o.buf.Bytes())).Receive()
	offer, _ := obj.(*nat.ReflectUDPOffer)
	ready := err == nil && offer != nil && !o.sent
	o.sent = o.sent || ready
	o.mu.Unlock()

	if ready {
		o.send(offer)
	}
	return len(b), nil
}

// punchOutcome is one side's result of a punch.
type punchOutcome struct {
	hole *nat.Hole
	err  error
	took time.Duration
}

// openSimPuncher opens n's punch socket and returns a puncher for session.
func openSimPuncher(t *testing.T, n *simNode, session []byte) *conePuncher {
	t.Helper()
	p, err := newConePuncher(session, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}
	p.conn, p.localPort = n.net.bind(t, n.host, n.port, n.nat), n.port
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// announce runs PunchProtocol.Announce for n, reflecting off n.reflector if set.
func announce(t *testing.T, n *simNode, proto *nat.PunchProtocol, p *conePuncher) {
	t.Helper()
	bindPort, err := p.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var r nat.Reflector
	if n.reflector != nil {
		r = &simReflector{conn: n.reflector, puncher: p}
	}
	if err := proto.Announce(astral.NewContext(context.Background()), bindPort, r); err != nil {
		t.Fatalf("Announce: %v", err)
	}
}

// simPunch runs the signal exchange of nat.punch between initiator and
// participant, with the steps of client.NodePunch and Module.OpNodePunch,
// and both HolePunch calls at once, as the go signal starts them.
func simPunch(t *testing.T, ctx context.Context, ini, par *simNode) (outI, outP punchOutcome) {
	t.Helper()
	advertised := func(n *simNode) ip.IP { return ip.IP(net.ParseIP(n.advertise).To4()) }

	protoI := nat.NewPunchProtocol(ini.id, par.id, advertised(ini))
	pi := openSimPuncher(t, ini, nil)
	announce(t, ini, protoI, pi)
	protoI.Session = pi.Session()

	protoP := nat.NewPunchProtocol(par.id, ini.id, advertised(par))
	_ = protoP.ExpectSignal(nat.PunchSignalTypeOffer, protoP.OnOffer)(protoI.OfferSignal())
	pp := openSimPuncher(t, par, protoP.Session)
	announce(t, par, protoP, pp)
	_ = protoI.ExpectSignal(nat.PunchSignalTypeAnswer, protoI.OnAnswer)(protoP.AnswerSignal())

	var wg sync.WaitGroup
	var resI, resP *nat.PunchResult
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		resI, outI.err = pi.HolePunch(ctx, protoI.PeerIP, int(protoI.PeerPort))
		outI.took = time.Since(start)
	}()
	go func() {
		defer wg.Done()
		start := time.Now()
		resP, outP.err = pp.HolePunch(ctx, protoP.PeerIP, int(protoP.PeerPort))
		outP.took = time.Since(start)
	}()
	wg.Wait()
	if outI.err != nil || outP.err != nil {
		return
	}

	protoI.SetPunchResult(resI)
	protoI.Hole.Nonce = astral.NewNonce()
	protoP.SetPunchResult(resP)
	_ = protoP.ExpectSignal(nat.PunchSignalTypeResult, protoP.OnResult)(protoI.ResultSignal())
	_ = protoI.ExpectSignal(nat.PunchSignalTypeResult, protoI.OnResult)(protoP.ResultSignal())

	_ = pi.Close()
	_ = pp.Close()
	outI.hole, outP.hole = &protoI.Hole, &protoP.Hole
	return
}

// echoOverHole opens kcp on each side's hole bind port, as NATLinkStrategy
// does after the hole handoff: the participant listens, the initiator dials
// the participant's external endpoint. It echoes count messages and returns
// how many came back.
func echoOverHole(t *testing.T, ini, par *simNode, holeI, holeP *nat.Hole, count int) int {
	t.Helper()
	connI := ini.net.bind(t, ini.host, holeI.GetLocalBindPort(ini.id), ini.nat)
	connP := par.net.bind(t, par.host, holeP.GetLocalBindPort(par.id), par.nat)

	l, err := kcpgo.ServeConn(nil, 0, 0, connP)
	if err != nil {
		t.Fatalf("kcp listen: %v", err)
	}
	defer l.Close()
	go func() {
		s, err := l.AcceptKCP()
		if err != nil {
			return
		}
		defer s.Close()
		_, _ = io.Copy(s, s)
	}()

	s, err := kcpgo.NewConn(holeI.GetRemoteAddr(ini.id).String(), nil, 0, 0, connI)
	if err != nil {
		t.Fatalf("kcp dial: %v", err)
	}
	defer s.Close()

	echoed := 0
	buf := make([]byte, 64)
	for i := 0; i < count; i++ {
		msg := []byte(fmt.Sprintf("echo %04d", i))
		if _, err := s.Write(msg); err != nil {
			t.Fatalf("kcp write: %v", err)
		}
		_ = s.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := io.ReadFull(s, buf[:len(msg)]); err != nil || !bytes.Equal(buf[:len(msg)], msg) {
			continue
		}
		echoed++
	}
	return echoed
}

const simEchoCount = 100

// assertPunched checks both sides punched within 1 s, that each hole binds
// the side's own punch port and names the peer at want, and that kcp over the
// hole echoes every message.
func assertPunched(t *testing.T, ini, par *simNode, outI, outP punchOutcome, wantI, wantP string) {
	t.Helper()
	for side, out := range map[string]punchOutcome{"initiator": outI, "participant": outP} {
		if out.err != nil {
			t.Fatalf("%s: HolePunch: %v", side, out.err)
		}
		if out.took >= time.Second {
			t.Errorf("%s: punch took %v; want under 1 s", side, out.took)
		}
	}

	if got := outI.hole.GetLocalBindPort(ini.id); got != ini.port {
		t.Errorf("initiator binds %d; want its punch port %d", got, ini.port)
	}
	if got := outP.hole.GetLocalBindPort(par.id); got != par.port {
		t.Errorf("participant binds %d; want its punch port %d", got, par.port)
	}
	if got := outI.hole.GetRemoteAddr(ini.id).String(); got != wantP {
		t.Errorf("initiator reaches the participant at %s; want %s", got, wantP)
	}
	if got := outP.hole.GetRemoteAddr(par.id).String(); got != wantI {
		t.Errorf("participant reaches the initiator at %s; want %s", got, wantI)
	}

	if got := echoOverHole(t, ini, par, outI.hole, outP.hole, simEchoCount); got != simEchoCount {
		t.Errorf("kcp over the hole echoed %d of %d messages", got, simEchoCount)
	}
}

func simIdentity() *astral.Identity { return astral.GenerateIdentity() }

// Step 1 (S1.4): a node behind a port-translating EIM NAT (local 37435 leaves
// as 18972, as measured behind Starlink) and a public peer punch, with or
// without a reflection, and kcp over the hole carries an echo.
func TestSimPunch_TranslatingNATToAPublicPeer(t *testing.T) {
	for _, reflect := range []bool{false, true} {
		t.Run(fmt.Sprintf("reflection=%v", reflect), func(t *testing.T) {
			n := newSimNet()
			starlink := n.addNAT("100.64.7.1", map[int]int{37435: 18972})
			natted := &simNode{id: simIdentity(), advertise: "100.64.7.1", host: "192.168.1.10", nat: starlink, port: 37435, net: n}
			public := &simNode{id: simIdentity(), advertise: "45.59.119.13", host: "45.59.119.13", port: 55001, net: n}
			if reflect {
				natted.reflector = n.bind(t, public.host, 47001, nil)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			outI, outP := simPunch(t, ctx, natted, public)
			assertPunched(t, natted, public, outI, outP, "100.64.7.1:18972", "45.59.119.13:55001")
		})
	}
}

// Step 1 (S1.5): two nodes behind port-preserving NATs, the nat-punch
// topology, still punch and carry kcp.
func TestSimPunch_PortPreservingNATs(t *testing.T) {
	n := newSimNet()
	natA := n.addNAT("198.51.100.10", nil)
	natB := n.addNAT("198.51.100.20", nil)
	a := &simNode{id: simIdentity(), advertise: "198.51.100.10", host: "10.0.1.2", nat: natA, port: 41000, net: n}
	b := &simNode{id: simIdentity(), advertise: "198.51.100.20", host: "10.0.2.2", nat: natB, port: 42000, net: n}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outI, outP := simPunch(t, ctx, a, b)
	assertPunched(t, a, b, outI, outP, "198.51.100.10:41000", "198.51.100.20:42000")
}

// Step 2 (S2.3): two nodes behind port-translating, port-restricted EIM NATs
// punch once each reflects its punch socket off a public node; without the
// reflection neither side ever reaches the other's mapped port.
func TestSimPunch_TwoTranslatingNATs(t *testing.T) {
	setup := func(t *testing.T, reflect bool) (n *simNet, a, b *simNode) {
		n = newSimNet()
		natA := n.addNAT("203.0.113.10", map[int]int{40001: 20001})
		natB := n.addNAT("203.0.113.20", map[int]int{40002: 20002})
		a = &simNode{id: simIdentity(), advertise: "203.0.113.10", host: "10.0.1.2", nat: natA, port: 40001, net: n}
		b = &simNode{id: simIdentity(), advertise: "203.0.113.20", host: "10.0.2.2", nat: natB, port: 40002, net: n}
		if reflect {
			a.reflector = n.bind(t, "45.59.119.13", 47001, nil)
			b.reflector = n.bind(t, "45.59.119.13", 47002, nil)
		}
		return
	}

	t.Run("reflected", func(t *testing.T) {
		_, a, b := setup(t, true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		outI, outP := simPunch(t, ctx, a, b)
		assertPunched(t, a, b, outI, outP, "203.0.113.10:20001", "203.0.113.20:20002")
	})

	t.Run("local ports only", func(t *testing.T) {
		n, a, b := setup(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		outI, outP := simPunch(t, ctx, a, b)
		if !errors.Is(outI.err, context.DeadlineExceeded) || !errors.Is(outP.err, context.DeadlineExceeded) {
			t.Fatalf("errors initiator %v, participant %v; want both deadline exceeded", outI.err, outP.err)
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.dropped == 0 {
			t.Error("the NATs dropped no probe; want every probe dropped")
		}
	})
}

// S2.4 in-process: frogger0 behind Starlink's translating NAT (42412 leaves as
// 36666, as measured) punches fold4 behind a port-preserving NAT at HQ and the
// public vps2, reflecting off vps2 as Module.reflectorFor picks it.
func TestSimPunch_StarlinkToHQAndVPS(t *testing.T) {
	n := newSimNet()
	starlink := n.addNAT("100.64.7.1", map[int]int{42412: 36666})
	hq := n.addNAT("212.105.131.237", nil)
	vps2 := "45.59.119.13"

	tests := []struct {
		name  string
		peer  *simNode
		wantP string
	}{
		{"fold4", &simNode{id: simIdentity(), advertise: "212.105.131.237", host: "10.10.0.4", nat: hq, port: 43000, net: n}, "212.105.131.237:43000"},
		{"vps2", &simNode{id: simIdentity(), advertise: vps2, host: vps2, port: 55001, net: n}, vps2 + ":55001"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frogger0 := &simNode{id: simIdentity(), advertise: "100.64.7.1", host: "192.168.1.10", nat: starlink, port: 42412, net: n}
			frogger0.reflector = n.bind(t, vps2, 47001+i, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			outI, outP := simPunch(t, ctx, frogger0, tt.peer)
			assertPunched(t, frogger0, tt.peer, outI, outP, "100.64.7.1:36666", tt.wantP)
		})
	}
}
