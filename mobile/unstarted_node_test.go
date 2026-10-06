package mobile

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

func TestUnstartedNodeAccessors(t *testing.T) {
	n := NewNode()

	joined := make(chan error, 1)
	go func() { joined <- n.Join() }()
	select {
	case err := <-joined:
		if err != nil {
			t.Fatalf("Join() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Join() on an unstarted node did not return")
	}

	n.Stop()

	if n.Running() {
		t.Error("Running() = true, want false")
	}
	if got := n.Identity(); got != "" {
		t.Errorf("Identity() = %q, want empty", got)
	}
	if got := n.Alias(); got != "" {
		t.Errorf("Alias() = %q, want empty", got)
	}
	if got, want := n.ApphostWSURL(), "ws://127.0.0.1:8624/.ws"; got != want {
		t.Errorf("ApphostWSURL() = %q, want %q", got, want)
	}
}

func TestUnstartedNodeStartWithoutConfig(t *testing.T) {
	if err := NewNode().Start(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Start() error = %v, want %v", err, ErrNotConfigured)
	}
}

func TestUnstartedNodeObjects(t *testing.T) {
	n := NewNode()
	const want = "node not running"

	if r, err := n.OpenObject("x", 0); err == nil || err.Error() != want {
		t.Errorf("OpenObject() = (%v, %v), want error %q", r, err, want)
	}
	if w, err := n.CreateObject(); err == nil || err.Error() != want {
		t.Errorf("CreateObject() = (%v, %v), want error %q", w, err, want)
	}
}

func TestQueryConnRead(t *testing.T) {
	t.Run("data", func(t *testing.T) {
		pr, pw := io.Pipe()
		defer pr.Close()
		c := &QueryConn{r: pr}

		go pw.Write([]byte("ab"))

		type readResult struct {
			n   int
			err error
		}
		buf := make([]byte, 8)
		done := make(chan readResult, 1)
		go func() {
			n, err := c.Read(buf)
			done <- readResult{n, err}
		}()

		select {
		case res := <-done:
			if res.n != 2 || res.err != nil || string(buf[:res.n]) != "ab" {
				t.Fatalf("Read = (%d, %q, %v), want (2, %q, nil)", res.n, buf[:res.n], res.err, "ab")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Read did not return in time")
		}
	})

	t.Run("closed", func(t *testing.T) {
		pr, pw := io.Pipe()
		c := &QueryConn{r: pr}
		pw.Close()

		n, err := c.Read(make([]byte, 8))
		if n != 0 || err != nil {
			t.Fatalf("Read after close = (%d, %v), want (0, nil)", n, err)
		}
	})

	t.Run("closed with error", func(t *testing.T) {
		pr, pw := io.Pipe()
		c := &QueryConn{r: pr}
		errX := errors.New("x")
		pw.CloseWithError(errX)

		n, err := c.Read(make([]byte, 8))
		if n != 0 || !errors.Is(err, errX) {
			t.Fatalf("Read after CloseWithError = (%d, %v), want (0, %v)", n, err, errX)
		}
	})

	t.Run("empty buffer", func(t *testing.T) {
		pr, _ := io.Pipe()
		defer pr.Close()
		c := &QueryConn{r: pr}

		n, err := c.Read(nil)
		if n != 0 || err != nil {
			t.Fatalf("Read(empty) = (%d, %v), want (0, nil)", n, err)
		}
	})
}

func TestObjectReaderRead(t *testing.T) {
	r := &ObjectReader{r: io.NopCloser(strings.NewReader("abc")), size: 3}
	buf := make([]byte, 8)

	n, err := r.Read(buf)
	if n != 3 || err != nil || string(buf[:n]) != "abc" {
		t.Fatalf("first Read = (%d, %q, %v), want (3, %q, nil)", n, buf[:n], err, "abc")
	}

	n, err = r.Read(buf)
	if n != 0 || err != nil {
		t.Fatalf("Read at end = (%d, %v), want (0, nil)", n, err)
	}

	if got := r.Size(); got != 3 {
		t.Fatalf("Size() = %d, want 3", got)
	}
}

func TestInboundQueryAccessors(t *testing.T) {
	id := secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
	q := astral.Launch(&astral.Query{Caller: id, QueryString: "player.play"})
	q.Extra.Set("origin", "network")
	q.Extra.Set("origin-web", "https://x")

	iq := newInboundQuery(q, nopWriteCloser{})

	if got := iq.Caller(); got != id.String() {
		t.Errorf("Caller() = %q, want %q", got, id.String())
	}
	if got := iq.Origin(); got != "network" {
		t.Errorf("Origin() = %q, want %q", got, "network")
	}
	if got := iq.OriginWeb(); got != "https://x" {
		t.Errorf("OriginWeb() = %q, want %q", got, "https://x")
	}
}
