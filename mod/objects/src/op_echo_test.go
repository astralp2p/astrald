package objects

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// recordingSender records every object passed to Send.
type recordingSender struct {
	sent []astral.Object
	err  error
}

func (s *recordingSender) Send(o astral.Object) error {
	s.sent = append(s.sent, o)
	return s.err
}

// countingReceiver wraps a Receiver, counts Receive calls, and—after a cap—forces io.EOF
// so a regression (an infinite re-read loop) terminates instead of hanging the test.
type countingReceiver struct {
	inner channel.Receiver
	cap   int
	calls int
}

func (c *countingReceiver) Receive() (astral.Object, error) {
	c.calls++
	if c.calls > c.cap {
		return nil, io.EOF
	}
	return c.inner.Receive()
}

// scriptedReceiver returns predefined (object, error) steps, then io.EOF.
type scriptedReceiver struct {
	steps []struct {
		obj astral.Object
		err error
	}
	i int
}

func (s *scriptedReceiver) Receive() (astral.Object, error) {
	if s.i >= len(s.steps) {
		return nil, io.EOF
	}
	st := s.steps[s.i]
	s.i++
	return st.obj, st.err
}

// canonicalUnknownType builds a CanonicalReceiver positioned on an unknown object type.
// Its first Receive returns ErrStreamCorrupted+ErrBlueprintNotFound and latches it, so
// every subsequent Receive returns the same error without consuming the reader — exactly
// the condition that made OpEcho spin.
func canonicalUnknownType(t *testing.T) channel.Receiver {
	t.Helper()
	var buf bytes.Buffer
	if _, err := (astral.Stamp{}).WriteTo(&buf); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if _, err := astral.ObjectType("unregistered.x.for.op_echo").WriteTo(&buf); err != nil {
		t.Fatalf("type: %v", err)
	}
	return channel.NewCanonicalReceiver(&buf)
}

// TestEcho_LatchedStreamError_Lenient_DoesNotSpin is the regression test for the live hang:
// a latching receiver returns ErrStreamCorrupted on every Receive, and lenient echo used to
// `continue` on it forever (100% CPU). The fix must stop after the first read.
func TestEcho_LatchedStreamError_Lenient_DoesNotSpin(t *testing.T) {
	rcv := &countingReceiver{inner: canonicalUnknownType(t), cap: 100}
	ch := &channel.Channel{Receiver: rcv, Sender: &recordingSender{}}

	done := make(chan error, 1)
	go func() { done <- echo(ch, opEchoArgs{}) }() // Strict=false (lenient)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("echo returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("echo did not return: spinning on latched stream error (Receive called %d times)", rcv.calls)
	}

	if rcv.calls != 1 {
		t.Fatalf("echo re-read the latched error %d times; want exactly 1 (must stop on ErrStreamCorrupted)", rcv.calls)
	}
}

// TestEcho_LatchedStreamError_Strict_ReportsInBand checks that strict mode surfaces the
// corruption to the peer (one error_message) and then stops, instead of spinning.
func TestEcho_LatchedStreamError_Strict_ReportsInBand(t *testing.T) {
	rcv := &countingReceiver{inner: canonicalUnknownType(t), cap: 100}
	snd := &recordingSender{}
	ch := &channel.Channel{Receiver: rcv, Sender: snd}

	done := make(chan error, 1)
	go func() { done <- echo(ch, opEchoArgs{Strict: true}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("echo returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("echo did not return in strict mode (Receive called %d times)", rcv.calls)
	}

	if rcv.calls != 1 {
		t.Fatalf("strict echo re-read the latched error %d times; want exactly 1", rcv.calls)
	}
	if len(snd.sent) != 1 {
		t.Fatalf("strict echo sent %d objects; want 1 in-band error", len(snd.sent))
	}
	if got := snd.sent[0].ObjectType(); got != (astral.ErrorMessage{}).ObjectType() {
		t.Fatalf("strict echo sent %q; want an error_message", got)
	}
}

// TestEcho_RelaysObjectsThenEOF guards the happy path through the extracted loop: objects
// are echoed back and a clean io.EOF ends the relay with a nil error.
func TestEcho_RelaysObjectsThenEOF(t *testing.T) {
	want := astral.NewError("payload")
	rcv := &scriptedReceiver{steps: []struct {
		obj astral.Object
		err error
	}{
		{obj: want},
		{err: io.EOF},
	}}
	snd := &recordingSender{}
	ch := &channel.Channel{Receiver: rcv, Sender: snd}

	if err := echo(ch, opEchoArgs{}); err != nil {
		t.Fatalf("echo returned error: %v", err)
	}
	if len(snd.sent) != 1 || snd.sent[0] != want {
		t.Fatalf("echo relayed %d objects; want exactly the received one", len(snd.sent))
	}
}
