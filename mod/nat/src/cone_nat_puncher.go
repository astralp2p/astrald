package nat

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/ip"
	"github.com/cryptopunkscc/astrald/mod/nat"
)

// Ensure conePuncher implements the public Puncher interface from the root package.
var _ nat.Puncher = (*conePuncher)(nil)

const (
	punchTimeout    = 10 * time.Second      // total timeout per attempt
	burstInterval   = 25 * time.Millisecond // time between bursts over the whole range
	portGuessRange  = 10                    // number of additional ports to probe around the base port (total ports = 2*portGuessRange + 1)
	packetsPerBurst = 5                     // number of packets sent to each address per burst
	jitterMax       = 5                     // max jitter in milliseconds between rounds
	minPort         = 1
	maxPort         = 1<<16 - 1
)

type ConePuncherCallbacks struct {
	OnAttempt       func(peer ip.IP, peerPort int, remoteAddrs []*net.UDPAddr)
	OnProbeReceived func(from *net.UDPAddr)
}

// conePuncher is a minimal cone NAT puncher using fixed defaults and a provided peer listen port.
// It's simplest form of punching as it only punches port and few ports around
// it. Which won't work on most of home NATs because they are asymmetric.
// TODO: research statistics about how many NATs are cone (with and without port preservation) and how many of them are asymmetric.
type conePuncher struct {
	session   []byte         // required session identifier (copied)
	conn      net.PacketConn // bound UDP socket
	localPort int            // cached local port of conn

	// callbacks
	callbacks *ConePuncherCallbacks
}

// newConePuncher creates a cone NAT puncher that adopts the provided session.
// Session must be exactly 16 bytes.
func newConePuncher(session []byte, cb *ConePuncherCallbacks) (puncher nat.Puncher, err error) {
	if len(session) != 0 && len(session) != 16 {
		return nil, fmt.Errorf("session must be 16 bytes")
	}

	if len(session) == 0 {
		session = make([]byte, 16)
		if _, err := crand.Read(session); err != nil {
			return nil, fmt.Errorf("generate session: %w", err)
		}
	}

	s := append([]byte(nil), session...) // safe copy

	p := &conePuncher{session: s, callbacks: cb}
	return p, nil
}

func (p *conePuncher) Session() []byte {
	return append([]byte(nil), p.session...)
}

// Open binds a UDP socket and stores it for later HolePunch use.
func (p *conePuncher) Open() (int, error) {
	if len(p.session) == 0 {
		return 0, errors.New("session is required")
	}

	lp := 0
	if p.conn != nil {
		lp = p.localPort
		return lp, nil
	}

	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return 0, fmt.Errorf("listen udp: %w", err)
	}
	lp = 0
	if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		lp = ua.Port
	}

	p.conn = conn
	p.localPort = lp
	return lp, nil
}

// Close releases any resources held by the puncher (open sockets).
func (p *conePuncher) Close() error {
	if p.conn != nil {
		err := p.conn.Close()
		p.conn = nil
		p.localPort = 0
		return err
	}
	p.localPort = 0
	return nil
}

// HolePunch attempts to punch a hole through NAT by sending UDP packets to candidate ports around peerPort.
// On success, returns the observed remote IP and port from the first matching incoming packet,
// which may differ from the announced peerIP/peerPort due to NAT translation.
func (p *conePuncher) HolePunch(
	ctx context.Context,
	peer ip.IP,
	peerPort int,
) (*nat.PunchResult, error) {
	if peer == nil || peer.String() == "" {
		return nil, errors.New("empty peer IP")
	}
	if len(p.session) == 0 {
		return nil, errors.New("session is required")
	}
	if peerPort < 1 || peerPort > 65535 {
		return nil, fmt.Errorf("invalid peer port: %d", peerPort)
	}

	// Snapshot conn and localPort
	conn := p.conn
	localPort := p.localPort

	if conn == nil {
		return nil, errors.New("no UDP connection available")
	}

	// Prepare candidate remote addresses around the peer's reported port.
	remoteAddrs, allowed, err := preparePunchTargets(peer, peerPort)
	if err != nil {
		return nil, err
	}
	// callback: plan
	if p.callbacks.OnAttempt != nil {
		p.callbacks.OnAttempt(peer, peerPort, remoteAddrs)
	}

	// Create a cancellable context to stop send/receive on success or timeout.
	ctx, cancel := context.WithTimeout(ctx, punchTimeout)
	defer cancel()

	// Launch sender and receiver using the snapshot conn.
	recvAddrCh := p.receive(ctx, conn, burstInterval, allowed)
	go p.sendBursts(ctx, conn, remoteAddrs, burstInterval)

	// Wait for success or timeout/cancel.
	select {
	case remoteAddr := <-recvAddrCh:
		return &nat.PunchResult{
			LocalPort:  astral.Uint16(localPort),
			RemoteIP:   ip.IP(remoteAddr.IP),
			RemotePort: astral.Uint16(remoteAddr.Port),
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// receive listens for any incoming UDP probe matching our session and sends the sender address to the returned channel.
func (p *conePuncher) receive(ctx context.Context, conn net.PacketConn, interval time.Duration, allowed *sig.Set[string]) <-chan *net.UDPAddr {
	var out = make(chan *net.UDPAddr, 1)

	go func() {
		defer close(out)

		buf := make([]byte, 1500)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(interval))
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}

				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}

				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(5 * time.Millisecond)
					continue
				}
			}

			ua, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}

			if !allowed.Contains(ua.String()) {
				continue
			}

			if n == len(p.session) && bytes.Equal(buf[:n], p.session) {
				if p.callbacks.OnProbeReceived != nil {
					p.callbacks.OnProbeReceived(ua)
				}
				select {
				case out <- ua:
				default:
				}
				return
			}
		}
	}()

	return out
}

// sendBursts sends the session payload to all candidate remoteAddrs simultaneously in each burst, and repeats until ctx is done.
func (p *conePuncher) sendBursts(
	ctx context.Context,
	conn net.PacketConn,
	remoteAddrs []*net.UDPAddr,
	burstEvery time.Duration,
) {
	ticker := time.NewTicker(burstEvery)
	defer ticker.Stop()

	burstIndex := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		for _, remoteAddr := range remoteAddrs {
			for i := 0; i < packetsPerBurst; i++ {
				_, _ = conn.WriteTo(p.session, remoteAddr)
			}
		}

		burstIndex++
		if jitterMax > 0 {
			time.Sleep(jitter(burstEvery, 3*time.Millisecond))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

	}
}

func candidatePorts(center, spread int) (ports []int) {
	spread = max(spread, 0)

	for i := max(center-spread, minPort); i < min(center+spread+1, maxPort); i++ {
		ports = append(ports, i)
	}
	return
}

// preparePunchTargets computes candidate ports, resolves remote addresses, and builds the allowed set.
// Returns ports, remoteAddrs, allowed set, and error.
func preparePunchTargets(peer ip.IP, peerPort int) ([]*net.UDPAddr, *sig.Set[string], error) {
	ports := candidatePorts(peerPort, portGuessRange)
	var remoteAddrs []*net.UDPAddr
	for _, rp := range ports {
		addr := net.JoinHostPort(peer.String(), strconv.Itoa(rp))
		remoteAddr, err := net.ResolveUDPAddr("udp", addr)
		if err == nil && remoteAddr != nil {
			remoteAddrs = append(remoteAddrs, remoteAddr)
		}
	}

	if len(remoteAddrs) == 0 {
		return nil, nil, errors.New("no remote addresses to probe")
	}

	// Build set of allowed remote endpoints for validation.
	var allowed sig.Set[string]
	for _, remoteAddr := range remoteAddrs {
		allowed.Add(remoteAddr.String())
	}

	return remoteAddrs, &allowed, nil
}

// jitter returns a random duration between base and base+max
func jitter(base, max time.Duration) time.Duration {
	if max <= 0 {
		return base
	}
	// Random value in [-max .. +max]
	delta := time.Duration(rand.Int63n(2*max.Nanoseconds()+1)) - max
	return base + delta
}
