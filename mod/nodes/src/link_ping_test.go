package nodes

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral/channel"
)

// rttRecorder records the RTT samples the link feeds the pressure detector.
//
// why no mutex: pong() runs on the test goroutine here.
type rttRecorder struct {
	samples []time.Duration
}

func (r *rttRecorder) OnBytes(int, time.Time)               {}
func (r *rttRecorder) IsHigh() bool                         { return false }
func (r *rttRecorder) OnRTT(rtt time.Duration, _ time.Time) { r.samples = append(r.samples, rtt) }

// TestPongDuringSendMeasuresRTTFromPublication pins the ping clock to the
// publication of the *Ping under pingMu: a pong handled while Send is still
// blocked yields an elapsed RTT, not time.Since of the zero time.
//
// why the Link is built by literal: newLink starts readLoop and pingLoop and
// needs a *Module, none of which this measurement needs.
func TestPongDuringSendMeasuresRTTFromPublication(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	rec := &rttRecorder{}
	link := &Link{
		Channel:     channel.New(local, channel.WithLockedWrites()),
		done:        make(chan struct{}),
		pingTimeout: time.Minute,
		pressure:    rec,
	}

	pinged := make(chan struct{})
	go func() {
		defer close(pinged)
		link.Ping()
	}()

	// why one byte: net.Pipe is unbuffered, so Write returns only once every byte
	// is consumed. Taking one pins Send mid-write -- the encoded ping frame is
	// longer than a byte -- while s.ping is already published.
	if _, err := remote.Read(make([]byte, 1)); err != nil {
		t.Fatalf("reading the ping frame: %v", err)
	}

	link.pingMu.Lock()
	p := link.ping
	link.pingMu.Unlock()
	if p == nil {
		t.Fatal("Ping() sent the frame without publishing the in-flight ping")
	}

	rtt, err := link.pong(p.nonce)
	if err != nil {
		t.Fatalf("pong(%v) = %v", p.nonce, err)
	}
	if rtt < 0 || rtt > time.Minute {
		t.Fatalf("pong RTT = %v; want a duration measured from the send, not from the zero time", rtt)
	}
	if len(rec.samples) != 1 || rec.samples[0] != rtt {
		t.Fatalf("pressure detector got %v; want exactly [%v]", rec.samples, rtt)
	}

	local.Close()
	select {
	case <-pinged:
	case <-time.After(effectTimeout):
		t.Fatal("Ping() did not return after the pipe closed")
	}
}
