package shell

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// note: readWriter has no Close method, so a Terminal over it cannot close its stream.
type readWriter struct {
	io.Reader
	io.Writer
}

type closingReadWriter struct {
	readWriter
	closed bool
	err    error
}

func (c *closingReadWriter) Close() error {
	c.closed = true
	return c.err
}

func TestTerminalReadLine(t *testing.T) {
	term := NewTerminal(readWriter{Reader: strings.NewReader("a\r\nb"), Writer: io.Discard})

	for _, want := range []string{"a", "b"} {
		line, err := term.ReadLine()
		if err != nil || line != want {
			t.Fatalf("ReadLine() = %q, %v; want %q, nil", line, err, want)
		}
	}

	line, err := term.ReadLine()
	if line != "" || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadLine() at the end = %q, %v; want \"\", %v", line, err, io.EOF)
	}
}

func TestTerminalReadLineTooLong(t *testing.T) {
	long := strings.Repeat("x", 70000) + "\n"
	term := NewTerminal(readWriter{Reader: strings.NewReader(long), Writer: io.Discard})

	if line, err := term.ReadLine(); !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("ReadLine() = %d bytes, %v; want %v", len(line), err, bufio.ErrTooLong)
	}
}

func TestTerminalClose(t *testing.T) {
	plain := NewTerminal(readWriter{Reader: strings.NewReader(""), Writer: io.Discard})
	if err := plain.Close(); err == nil {
		t.Fatal("Close() on a stream without Close = nil, want an error")
	}

	errClose := errors.New("close failed")
	rw := &closingReadWriter{readWriter: readWriter{Reader: strings.NewReader(""), Writer: io.Discard}, err: errClose}
	if err := NewTerminal(rw).Close(); !errors.Is(err, errClose) {
		t.Fatalf("Close() = %v, want the stream's %v", err, errClose)
	}
	if !rw.closed {
		t.Fatal("Close() did not close the underlying stream")
	}
}

func TestTerminalPrintf(t *testing.T) {
	var out bytes.Buffer
	term := NewTerminal(readWriter{Reader: strings.NewReader(""), Writer: &out})

	if err := term.Printf("x=%d\n", 5); err != nil {
		t.Fatalf("Printf() = %v, want nil", err)
	}
	if got, want := out.String(), "x=5\n"; got != want {
		t.Fatalf("Printf wrote %q, want %q", got, want)
	}
}
