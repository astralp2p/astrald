package brontide

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

const zeroRecordTimeout = 10 * time.Second

// zeroRecordPair completes a handshake over net.Pipe and returns the initiator's
// and responder's ends.
func zeroRecordPair(t *testing.T) (initiator, responder *Conn) {
	t.Helper()

	responderKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("responder key: %v", err)
	}
	initiatorKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("initiator key: %v", err)
	}

	local, remote := net.Pipe()
	deadline := time.Now().Add(zeroRecordTimeout)
	if err := local.SetDeadline(deadline); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := remote.SetDeadline(deadline); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	type result struct {
		conn *Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := PassiveHandshake(remote, responderKey)
		done <- result{c, err}
	}()

	initiator, err = ActiveHandshake(local, initiatorKey, responderKey.PubKey())
	if err != nil {
		t.Fatalf("active handshake: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("passive handshake: %v", r.err)
		}
		responder = r.conn
	case <-time.After(zeroRecordTimeout):
		t.Fatal("passive handshake did not complete")
	}

	t.Cleanup(func() {
		initiator.Close()
		responder.Close()
	})
	return initiator, responder
}

// TestConnReadSkipsZeroLengthRecords: a peer may legally send a record whose
// payload is empty. Read must fetch the next record rather than report io.EOF
// on a stream that is still open.
//
// why three empty records in a row: it proves the loop consumes records rather
// than spinning on an empty buffer.
func TestConnReadSkipsZeroLengthRecords(t *testing.T) {
	initiator, responder := zeroRecordPair(t)

	writeErr := make(chan error, 1)
	go func() {
		for i := 0; i < 3; i++ {
			if _, err := initiator.Write(nil); err != nil {
				writeErr <- err
				return
			}
		}
		_, err := initiator.Write([]byte("hello"))
		writeErr <- err
	}()

	buf := make([]byte, 16)
	n, err := responder.Read(buf)
	if err != nil {
		t.Fatalf("Read after three zero-length records = %v; want the next payload", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Errorf("Read returned %q; want %q", got, "hello")
	}

	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	case <-time.After(zeroRecordTimeout):
		t.Fatal("writer did not finish")
	}
}

// TestConnReadReturnsEOFAfterPeerClose holds the genuine end-of-stream path: the
// loop must not swallow a real EOF.
func TestConnReadReturnsEOFAfterPeerClose(t *testing.T) {
	initiator, responder := zeroRecordPair(t)

	if err := initiator.Close(); err != nil {
		t.Fatalf("close initiator: %v", err)
	}

	read := make(chan error, 1)
	go func() {
		_, err := responder.Read(make([]byte, 16))
		read <- err
	}()

	select {
	case err := <-read:
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Read after the peer closed = %v; want io.EOF", err)
		}
	case <-time.After(zeroRecordTimeout):
		t.Fatal("Read did not return after the peer closed")
	}
}
