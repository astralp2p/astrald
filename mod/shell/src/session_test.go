package shell

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

type scriptedConn struct {
	io.Reader
	out bytes.Buffer
}

func (c *scriptedConn) Write(p []byte) (int, error) { return c.out.Write(p) }

func (c *scriptedConn) Close() error { return nil }

type sessionRun struct {
	err    error
	out    string
	prompt string
}

func runScript(t *testing.T, script string) sessionRun {
	t.Helper()

	mod := testModule(t)
	guest := astral.GenerateIdentity()
	conn := &scriptedConn{Reader: strings.NewReader(script)}
	prompt := (&Prompt{GuestID: guest, HostID: mod.node.Identity()}).Render()

	// why: NewSession reads through streams.ContextReader, which races on its channel field under -race.
	session := &Session{mod: mod, rwc: conn}

	result := make(chan error, 1)
	go func() {
		result <- session.Run(astral.NewContext(nil).WithIdentity(guest))
	}()

	select {
	case err := <-result:
		return sessionRun{err: err, out: conn.out.String(), prompt: prompt}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
		return sessionRun{}
	}
}

func TestSessionRepromptsAfterABlankOrMisquotedLineUntilExit(t *testing.T) {
	run := runScript(t, "\n\"abc\nexit\n")

	if run.err != nil {
		t.Fatalf("Run() = %v, want nil after exit", run.err)
	}
	if want := run.prompt + run.prompt + "quote mismatch\n" + run.prompt; run.out != want {
		t.Fatalf("output = %q, want %q", run.out, want)
	}
}

func TestSessionReportsARoutingErrorAndReadsOn(t *testing.T) {
	run := runScript(t, "foo bar=1\n")

	if !errors.Is(run.err, io.EOF) {
		t.Fatalf("Run() = %v, want %v", run.err, io.EOF)
	}
	if want := run.prompt + "error: route not found\n" + run.prompt; run.out != want {
		t.Fatalf("output = %q, want %q", run.out, want)
	}
}

func TestSessionEndsOnEOF(t *testing.T) {
	run := runScript(t, "")

	if !errors.Is(run.err, io.EOF) {
		t.Fatalf("Run() = %v, want %v", run.err, io.EOF)
	}
	if run.out != run.prompt {
		t.Fatalf("output = %q, want exactly one prompt %q", run.out, run.prompt)
	}
}
