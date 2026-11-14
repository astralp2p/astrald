// Command driver is the fake peer of basic-strategy-stalled-negotiation: an
// identity with no astrald behind it, served on three TCP listeners on the
// test host.
//
// Every listener completes the Noise_XK responder handshake (brontide) with
// the peer's key. The stall listener then sends nothing and reads until the
// dialer closes. The live listeners run the responder side of the mux2
// negotiation (astrald mod/nodes/src/link_negotiator.go NegotiateInbound) and
// then hold the link, answering pings so the dialer keeps it. A live listener
// starts its negotiation only once a stall connection sits open after its
// handshake, or after stallWait, so the round's winner always arrives while a
// sibling dial is stuck in mux negotiation.
//
// It writes its state to the JSON file named by ASTRAL_TESTS_APP_STATE: the
// identity, each listener's port, and one row per accepted connection, with
// times on the host clock in seconds since the epoch. The file can lag the
// connections on a loaded disk; the write at SIGTERM is complete and marks
// itself final.
//
// Environment: FAKEPEER_HOST is the address it binds (the dialing node's
// host_ip).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/brontide"
	"github.com/astralp2p/astrald/mod/nodes/frames"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const featureMux2 = "mux2"

// stallWait bounds how long a live listener waits for a stalled sibling.
const stallWait = 3 * time.Second

// listeners maps each listener name to whether it stalls after the handshake.
var listeners = []struct {
	name  string
	stall bool
}{{"stall", true}, {"live2", false}, {"live3", false}}

// conn is one accepted connection as the oracle reads it.
type conn struct {
	Listener   string   `json:"listener"`
	AcceptedAt float64  `json:"accepted_at"`
	Handshaked *float64 `json:"handshaked_at"`
	Negotiated *float64 `json:"negotiated_at"`
	StallSeen  bool     `json:"stall_seen"` // a stall connection sat open after its handshake when negotiation began
	Pings      int      `json:"pings"`
	ClosedAt   *float64 `json:"closed_at"`
	Error      string   `json:"error,omitempty"`
}

type state struct {
	mu       sync.Mutex
	path     string
	Identity string         `json:"identity"`
	Ports    map[string]int `json:"ports"`
	Conns    []*conn        `json:"conns"`
	Final    bool           `json:"final"` // set by the last write, at exit
	dirty    chan struct{}
}

func now() *float64 {
	t := float64(time.Now().UnixNano()) / 1e9
	return &t
}

// update applies f under the lock and asks the writer for a new state file.
//
// why a separate writer: a file write can block for seconds on a loaded disk,
// and the connections must not wait for it to record their times or move on.
func (s *state) update(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// write writes the current state to the state file.
func (s *state) write() {
	s.mu.Lock()
	raw, _ := json.Marshal(s)
	s.mu.Unlock()
	// why rename: the reader polls the file and must never see half of it
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

// writeLoop writes the state file after every burst of updates, until done closes.
func (s *state) writeLoop(done <-chan struct{}) {
	for {
		select {
		case <-s.dirty:
			s.write()
		case <-done:
			s.update(func() { s.Final = true })
			s.write()
			return
		}
	}
}

// stalled reports whether a stall connection sits open after its handshake.
func (s *state) stalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.Conns {
		if c.Listener == "stall" && c.Handshaked != nil && c.ClosedAt == nil {
			return true
		}
	}
	return false
}

// awaitStall waits up to stallWait for a stalled sibling; whether one came.
func (s *state) awaitStall() bool {
	deadline := time.Now().Add(stallWait)
	for !s.stalled() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

// negotiate runs the responder side of the mux2 negotiation.
func negotiate(ch *channel.Channel) error {
	if err := ch.Send(astral.NewUint16(1)); err != nil {
		return err
	}
	if err := ch.Send(astral.NewString8(featureMux2)); err != nil {
		return err
	}
	obj, err := ch.Receive()
	if err != nil {
		return fmt.Errorf("read selected feature: %w", err)
	}
	if f, ok := obj.(*astral.String8); !ok || f.String() != featureMux2 {
		return fmt.Errorf("selected feature %v, not %v", obj, featureMux2)
	}
	if err := ch.Send(astral.NewUint8(0)); err != nil {
		return err
	}
	nonce := astral.NewNonce()
	return ch.Send(&nonce)
}

// hold answers pings until the dialer closes the link.
func hold(s *state, c *conn, ch *channel.Channel) error {
	for {
		obj, err := ch.Receive()
		if err != nil {
			return err
		}
		if p, ok := obj.(*frames.Ping); ok && !bool(p.Pong) {
			s.update(func() { c.Pings++ })
			if err := ch.Send(&frames.Ping{Nonce: p.Nonce, Pong: true}); err != nil {
				return err
			}
		}
	}
}

// discard reads until the dialer closes; the stall listener's whole answer.
func discard(b *brontide.Conn) error {
	buf := make([]byte, 4096)
	for {
		if _, err := b.Read(buf); err != nil {
			return err
		}
	}
}

func serveConn(s *state, key *secp256k1.PrivateKey, name string, stall bool, nc net.Conn) {
	defer nc.Close()
	c := &conn{Listener: name, AcceptedAt: *now()}
	s.update(func() { s.Conns = append(s.Conns, c) })
	var err error
	defer func() {
		s.update(func() {
			c.ClosedAt = now()
			if err != nil {
				c.Error = err.Error()
			}
		})
	}()

	b, err := brontide.PassiveHandshake(nc, key)
	if err != nil {
		return
	}
	s.update(func() { c.Handshaked = now() })
	if stall {
		err = discard(b)
		return
	}
	seen := s.awaitStall()
	s.update(func() { c.StallSeen = seen })
	ch := channel.New(b)
	if err = negotiate(ch); err != nil {
		return
	}
	s.update(func() { c.Negotiated = now() })
	err = hold(s, c, ch)
}

func serve(s *state, key *secp256k1.PrivateKey, l net.Listener, name string, stall bool) {
	for {
		nc, err := l.Accept()
		if err != nil {
			return
		}
		go serveConn(s, key, name, stall, nc)
	}
}

func run() error {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	host := os.Getenv("FAKEPEER_HOST")
	if path == "" || host == "" {
		return errors.New("ASTRAL_TESTS_APP_STATE and FAKEPEER_HOST must be set")
	}
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return err
	}
	s := &state{path: path, Ports: map[string]int{}, Conns: []*conn{},
		Identity: astral.IdentityFromPubKey(key.PubKey()).String(),
		dirty:    make(chan struct{}, 1)}
	done := make(chan struct{})
	written := make(chan struct{})
	go func() {
		defer close(written)
		s.writeLoop(done)
	}()

	for _, spec := range listeners {
		l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return err
		}
		defer l.Close()
		_, port, _ := net.SplitHostPort(l.Addr().String())
		n, _ := strconv.Atoi(port)
		s.update(func() { s.Ports[spec.name] = n })
		go serve(s, key, l, spec.name, spec.stall)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	<-sigs
	close(done)
	<-written
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakepeer:", err)
		os.Exit(1)
	}
}
