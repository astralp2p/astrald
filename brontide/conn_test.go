package brontide

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

// why: the guard only catches a hang, and a cold handshake under -race took 0.35s on a Raspberry Pi 4.
const pipeTimeout = 10 * time.Second

type handshakeResult struct {
	conn *Conn
	err  error
}

func startPassive(conn net.Conn, key *btcec.PrivateKey) <-chan handshakeResult {
	ch := make(chan handshakeResult, 1)
	go func() {
		c, err := PassiveHandshake(conn, key)
		ch <- handshakeResult{c, err}
	}()
	return ch
}

func awaitPassive(t *testing.T, ch <-chan handshakeResult) handshakeResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(pipeTimeout):
		t.Fatal("PassiveHandshake did not return in time")
		return handshakeResult{}
	}
}

func newDeadlinePipe(t *testing.T) (a, b net.Conn) {
	t.Helper()
	a, b = net.Pipe()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	// why: a deadline turns a stuck pipe read or write into a test failure instead of a hang.
	deadline := time.Now().Add(pipeTimeout)
	a.SetDeadline(deadline)
	b.SetDeadline(deadline)
	return a, b
}

func newConnPair(t *testing.T) (ca, cb *Conn, ka, kb *btcec.PrivateKey) {
	t.Helper()
	ka, kb = newTestKey(t), newTestKey(t)
	a, b := newDeadlinePipe(t)

	passive := startPassive(b, kb)
	ca, err := ActiveHandshake(a, ka, kb.PubKey())
	if err != nil {
		t.Fatalf("ActiveHandshake: %v", err)
	}
	res := awaitPassive(t, passive)
	if res.err != nil {
		t.Fatalf("PassiveHandshake: %v", res.err)
	}
	return ca, res.conn, ka, kb
}

type writeResult struct {
	n   int
	err error
}

func writeAsync(c *Conn, p []byte) <-chan writeResult {
	ch := make(chan writeResult, 1)
	go func() {
		n, err := c.Write(p)
		ch <- writeResult{n, err}
	}()
	return ch
}

func awaitWrite(t *testing.T, ch <-chan writeResult) writeResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(pipeTimeout):
		t.Fatal("Write did not return in time")
		return writeResult{}
	}
}

func TestHandshakeOverPipe(t *testing.T) {
	ca, cb, ka, kb := newConnPair(t)

	if !ca.RemotePub().IsEqual(kb.PubKey()) {
		t.Errorf("active RemotePub = %x, want %x", ca.RemotePub().SerializeCompressed(), kb.PubKey().SerializeCompressed())
	}
	if !cb.RemotePub().IsEqual(ka.PubKey()) {
		t.Errorf("passive RemotePub = %x, want %x", cb.RemotePub().SerializeCompressed(), ka.PubKey().SerializeCompressed())
	}
	if !ca.LocalPub().IsEqual(ka.PubKey()) {
		t.Errorf("active LocalPub = %x, want %x", ca.LocalPub().SerializeCompressed(), ka.PubKey().SerializeCompressed())
	}
}

func TestConnWriteChunksLargePayload(t *testing.T) {
	ca, cb, _, _ := newConnPair(t)

	payload := make([]byte, 70000)
	for i := range payload {
		payload[i] = byte(i)
	}

	written := writeAsync(ca, payload)

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(cb, got); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if res := awaitWrite(t, written); res.n != len(payload) || res.err != nil {
		t.Fatalf("Write(70000 bytes) = (%d, %v), want (%d, nil)", res.n, res.err, len(payload))
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("received payload differs from the written payload")
	}
}

func TestConnReadServesRemainderFromBuffer(t *testing.T) {
	ca, cb, _, _ := newConnPair(t)

	written := writeAsync(ca, []byte("hello"))

	buf := make([]byte, 3)
	n, err := cb.Read(buf)
	if err != nil || string(buf[:n]) != "hel" {
		t.Fatalf("first Read = (%q, %v), want (%q, nil)", buf[:n], err, "hel")
	}
	if res := awaitWrite(t, written); res.n != 5 || res.err != nil {
		t.Fatalf("Write(hello) = (%d, %v), want (5, nil)", res.n, res.err)
	}

	n, err = cb.Read(buf)
	if err != nil || string(buf[:n]) != "lo" {
		t.Fatalf("second Read = (%q, %v), want (%q, nil)", buf[:n], err, "lo")
	}
}

func TestPassiveHandshakeRejectsWrongKey(t *testing.T) {
	ka, kb := newTestKey(t), newTestKey(t)
	a, b := newDeadlinePipe(t)

	passive := startPassive(b, kb)
	if _, err := ActiveHandshake(a, ka, newTestKey(t).PubKey()); err == nil {
		t.Error("ActiveHandshake against a responder with another key: got nil error, want non-nil")
	}
	res := awaitPassive(t, passive)
	if res.err == nil {
		t.Fatal("PassiveHandshake with a wrong initiator target key: got nil error, want non-nil")
	}
	if _, err := b.Write([]byte{0}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write on the passive pipe end after rejection: error = %v, want %v", err, io.ErrClosedPipe)
	}
}

func TestPassiveHandshakePeerClosed(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { b.Close() })
	a.Close()

	res := awaitPassive(t, startPassive(b, newTestKey(t)))
	if res.err == nil {
		t.Fatal("PassiveHandshake after the peer closed: got nil error, want non-nil")
	}
}
