package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// relay forwards every TCP connection it accepts to target until it is
// black-holed; from then on it drops every byte in both directions and keeps
// every connection open, as a path that vanished without a reset does.
// note: copied from tests/e2e/reconnect-sync-after-dead-link/driver; a second
// user does not make a shared package (plan decision D4).
type relay struct {
	ln     net.Listener
	target string
	dead   atomic.Bool
	// lastByte is the unix nanoseconds of the last write that forwarded bytes.
	lastByte atomic.Int64
	// forwarded counts the bytes forwarded in both directions.
	forwarded atomic.Uint64
}

// startRelay listens at host on a free port and forwards to target.
func startRelay(host, target string) (*relay, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, fmt.Errorf("relay listen at %v: %w", host, err)
	}
	r := &relay{ln: ln, target: target}
	r.lastByte.Store(time.Now().UnixNano())
	go r.serve()
	return r, nil
}

func (r *relay) Addr() string { return r.ln.Addr().String() }

func (r *relay) Close() error { return r.ln.Close() }

func (r *relay) blackhole() { r.dead.Store(true) }

// last is the time of the last forwarded byte.
func (r *relay) last() time.Time { return time.Unix(0, r.lastByte.Load()) }

// waitQuiet reports whether the relay forwarded no byte for quiet within limit.
func (r *relay) waitQuiet(quiet, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if time.Since(r.last()) >= quiet {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (r *relay) serve() {
	for {
		down, err := r.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", r.target)
		if err != nil {
			down.Close()
			continue
		}
		go r.pipe(up, down)
		go r.pipe(down, up)
	}
}

// pipe copies src to dst until the relay is black-holed, then reads src and
// drops what it reads.
// why the close is not passed on once black-holed: a vanished path delivers
// no FIN either.
func (r *relay) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if r.dead.Load() {
			if err != nil {
				return
			}
			continue
		}
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				src.Close()
				return
			}
			r.lastByte.Store(time.Now().UnixNano())
			r.forwarded.Add(uint64(n))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, "relay:", err)
			}
			dst.Close()
			return
		}
	}
}
