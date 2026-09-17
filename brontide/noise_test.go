package brontide

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
)

func newTestKey(t *testing.T) *btcec.PrivateKey {
	t.Helper()
	k, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}
	return k
}

func newTestMachines(t *testing.T) (initiator, responder *Machine, kI, kR *btcec.PrivateKey) {
	t.Helper()
	kI, kR = newTestKey(t), newTestKey(t)
	initiator = NewBrontideMachine(true, &PrivKeyECDH{kI}, kR.PubKey())
	responder = NewBrontideMachine(false, &PrivKeyECDH{kR}, nil)
	return initiator, responder, kI, kR
}

func completeHandshake(t *testing.T, initiator, responder *Machine) {
	t.Helper()
	actOne, err := initiator.GenActOne()
	if err != nil {
		t.Fatalf("GenActOne: %v", err)
	}
	if err := responder.RecvActOne(actOne); err != nil {
		t.Fatalf("RecvActOne: %v", err)
	}
	actTwo, err := responder.GenActTwo()
	if err != nil {
		t.Fatalf("GenActTwo: %v", err)
	}
	if err := initiator.RecvActTwo(actTwo); err != nil {
		t.Fatalf("RecvActTwo: %v", err)
	}
	actThree, err := initiator.GenActThree()
	if err != nil {
		t.Fatalf("GenActThree: %v", err)
	}
	if err := responder.RecvActThree(actThree); err != nil {
		t.Fatalf("RecvActThree: %v", err)
	}
}

func sendMessage(t *testing.T, from, to *Machine, msg []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := from.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	n, err := from.Flush(&buf)
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("Flush returned %d, want %d", n, len(msg))
	}
	got, err := to.ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return got
}

func TestMachineHandshakeAuthenticatesInitiator(t *testing.T) {
	initiator, responder, kI, _ := newTestMachines(t)

	completeHandshake(t, initiator, responder)

	if responder.remoteStatic == nil || !responder.remoteStatic.IsEqual(kI.PubKey()) {
		t.Fatalf("responder remoteStatic = %v, want initiator key %v", responder.remoteStatic, kI.PubKey())
	}
}

func TestMachineMessageRoundTrip(t *testing.T) {
	initiator, responder, _, _ := newTestMachines(t)
	completeHandshake(t, initiator, responder)

	if got := sendMessage(t, initiator, responder, []byte("hello")); string(got) != "hello" {
		t.Fatalf("initiator->responder got %q, want %q", got, "hello")
	}
	if got := sendMessage(t, responder, initiator, []byte("hello")); string(got) != "hello" {
		t.Fatalf("responder->initiator got %q, want %q", got, "hello")
	}
}

func TestMachineKeyRotation(t *testing.T) {
	initiator, responder, _, _ := newTestMachines(t)
	completeHandshake(t, initiator, responder)

	// note: each message advances the nonce twice (header and body), so 1005 messages rotate each cipherState twice.
	const count = 1005
	for i := 0; i < count; i++ {
		msg := []byte(fmt.Sprintf("message %d", i))
		if got := sendMessage(t, initiator, responder, msg); !bytes.Equal(got, msg) {
			t.Fatalf("initiator->responder message %d: got %q, want %q", i, got, msg)
		}
		if got := sendMessage(t, responder, initiator, msg); !bytes.Equal(got, msg) {
			t.Fatalf("responder->initiator message %d: got %q, want %q", i, got, msg)
		}
	}
}

func TestMachineWrongResponderKey(t *testing.T) {
	kI, kR := newTestKey(t), newTestKey(t)
	initiator := NewBrontideMachine(true, &PrivKeyECDH{kI}, newTestKey(t).PubKey())
	responder := NewBrontideMachine(false, &PrivKeyECDH{kR}, nil)

	actOne, err := initiator.GenActOne()
	if err != nil {
		t.Fatalf("GenActOne: %v", err)
	}
	if err := responder.RecvActOne(actOne); err == nil {
		t.Fatal("RecvActOne with an act for another responder key: got nil error, want non-nil")
	}
}

func TestMachineInvalidHandshakeVersion(t *testing.T) {
	const want = "invalid handshake version"

	t.Run("act one", func(t *testing.T) {
		initiator, responder, _, _ := newTestMachines(t)
		actOne, err := initiator.GenActOne()
		if err != nil {
			t.Fatalf("GenActOne: %v", err)
		}
		actOne[0] = 1
		err = responder.RecvActOne(actOne)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("RecvActOne error = %v, want containing %q", err, want)
		}
	})

	t.Run("act two", func(t *testing.T) {
		initiator, responder, _, _ := newTestMachines(t)
		actOne, err := initiator.GenActOne()
		if err != nil {
			t.Fatalf("GenActOne: %v", err)
		}
		if err := responder.RecvActOne(actOne); err != nil {
			t.Fatalf("RecvActOne: %v", err)
		}
		actTwo, err := responder.GenActTwo()
		if err != nil {
			t.Fatalf("GenActTwo: %v", err)
		}
		actTwo[0] = 1
		err = initiator.RecvActTwo(actTwo)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("RecvActTwo error = %v, want containing %q", err, want)
		}
	})

	t.Run("act three", func(t *testing.T) {
		initiator, responder, _, _ := newTestMachines(t)
		actOne, err := initiator.GenActOne()
		if err != nil {
			t.Fatalf("GenActOne: %v", err)
		}
		if err := responder.RecvActOne(actOne); err != nil {
			t.Fatalf("RecvActOne: %v", err)
		}
		actTwo, err := responder.GenActTwo()
		if err != nil {
			t.Fatalf("GenActTwo: %v", err)
		}
		if err := initiator.RecvActTwo(actTwo); err != nil {
			t.Fatalf("RecvActTwo: %v", err)
		}
		actThree, err := initiator.GenActThree()
		if err != nil {
			t.Fatalf("GenActThree: %v", err)
		}
		actThree[0] = 1
		err = responder.RecvActThree(actThree)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("RecvActThree error = %v, want containing %q", err, want)
		}
	})
}

func TestMachineReadMessageRejectsTamperedBody(t *testing.T) {
	initiator, responder, _, _ := newTestMachines(t)
	completeHandshake(t, initiator, responder)

	var buf bytes.Buffer
	if err := initiator.WriteMessage([]byte("hello")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if _, err := initiator.Flush(&buf); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	buf.Bytes()[encHeaderSize] ^= 0x01

	if msg, err := responder.ReadMessage(&buf); err == nil {
		t.Fatalf("ReadMessage of a tampered body = (%q, nil), want non-nil error", msg)
	}
}

func TestMachineWriteMessageErrors(t *testing.T) {
	initiator, responder, _, _ := newTestMachines(t)
	completeHandshake(t, initiator, responder)

	if err := initiator.WriteMessage(make([]byte, 65536)); !errors.Is(err, ErrMaxMessageLengthExceeded) {
		t.Fatalf("WriteMessage(65536 bytes) error = %v, want %v", err, ErrMaxMessageLengthExceeded)
	}
	if err := initiator.WriteMessage([]byte("a")); err != nil {
		t.Fatalf("WriteMessage(a): %v", err)
	}
	if err := initiator.WriteMessage([]byte("b")); !errors.Is(err, ErrMessageNotFlushed) {
		t.Fatalf("WriteMessage(b) before Flush error = %v, want %v", err, ErrMessageNotFlushed)
	}
}

var errShortWrite = errors.New("short write")

// why: a short write returns an error to keep the io.Writer contract that Flush resumes from.
type cappedWriter struct {
	buf bytes.Buffer
	max int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if len(p) <= w.max {
		return w.buf.Write(p)
	}
	n, _ := w.buf.Write(p[:w.max])
	return n, errShortWrite
}

func TestMachineFlushPartialWrites(t *testing.T) {
	initiator, responder, _, _ := newTestMachines(t)
	completeHandshake(t, initiator, responder)

	payload := []byte("twenty bytes payload")
	if err := initiator.WriteMessage(payload); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	w := &cappedWriter{max: 7}
	total := 0
	flushed := false
	// why: header, payload and MAC add up to 54 bytes, so 7-byte writes need at most 8 calls.
	for i := 0; i < 20; i++ {
		n, err := initiator.Flush(w)
		total += n
		if err == nil {
			flushed = true
			break
		}
		if !errors.Is(err, errShortWrite) {
			t.Fatalf("Flush error = %v, want %v", err, errShortWrite)
		}
	}
	if !flushed {
		t.Fatal("Flush did not complete within 20 calls")
	}
	if total != len(payload) {
		t.Fatalf("sum of Flush counts = %d, want %d", total, len(payload))
	}

	n, err := initiator.Flush(w)
	if n != 0 || err != nil {
		t.Fatalf("Flush with nothing pending = (%d, %v), want (0, nil)", n, err)
	}

	got, err := responder.ReadMessage(&w.buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("ReadMessage got %q, want %q", got, payload)
	}
}

func TestMachineEphemeralGenerator(t *testing.T) {
	kI, kR, kE := newTestKey(t), newTestKey(t), newTestKey(t)
	gen := EphemeralGenerator(func() (*btcec.PrivateKey, error) { return kE, nil })

	a := NewBrontideMachine(true, &PrivKeyECDH{kI}, kR.PubKey(), gen)
	b := NewBrontideMachine(true, &PrivKeyECDH{kI}, kR.PubKey(), gen)

	actA, err := a.GenActOne()
	if err != nil {
		t.Fatalf("GenActOne a: %v", err)
	}
	actB, err := b.GenActOne()
	if err != nil {
		t.Fatalf("GenActOne b: %v", err)
	}
	if actA != actB {
		t.Fatalf("GenActOne outputs differ with a fixed ephemeral key:\n a=%x\n b=%x", actA, actB)
	}
}
