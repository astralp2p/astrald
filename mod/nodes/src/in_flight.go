package nodes

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// inFlight counts the link attempts in progress with each peer.
// why a mutex: a count and its idle channel change together.
type inFlight struct {
	mu    sync.Mutex
	peers map[string]*peerAttempts
}

// peerAttempts is the in-progress count for one peer; idle closes when it drops to zero, ended at every attempt that ends.
type peerAttempts struct {
	n     int
	idle  chan struct{}
	ended chan struct{}
}

// begin records an attempt with peer and returns the func that ends it.
func (ns *inFlight) begin(peer *astral.Identity) (end func()) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if ns.peers == nil {
		ns.peers = map[string]*peerAttempts{}
	}
	key := peer.String()
	p, ok := ns.peers[key]
	if !ok {
		p = &peerAttempts{idle: make(chan struct{}), ended: make(chan struct{})}
		ns.peers[key] = p
	}
	p.n++
	return func() { ns.end(key, p) }
}

// end drops one attempt of p, closes its ended channel, and closes its idle channel at zero.
func (ns *inFlight) end(key string, p *peerAttempts) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	p.n--
	close(p.ended)
	p.ended = make(chan struct{})
	if p.n == 0 {
		close(p.idle)
		delete(ns.peers, key)
	}
}

// idle returns a channel closed once no attempt with peer is in progress.
func (ns *inFlight) idle(peer *astral.Identity) <-chan struct{} {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if p, ok := ns.peers[peer.String()]; ok {
		return p.idle
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

// ended returns a channel closed once the next attempt with peer ends; closed at once when none is in progress.
func (ns *inFlight) ended(peer *astral.Identity) <-chan struct{} {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if p, ok := ns.peers[peer.String()]; ok {
		return p.ended
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}
