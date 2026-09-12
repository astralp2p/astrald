package coldcard

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
)

// installCkccSubstitute puts a stand-in for the ckcc device tool alone on PATH.
// The stand-in lists no device and appends each invocation's arguments to a log,
// so a test observes whether the op reached the device layer without a Coldcard
// attached. The returned function reads that log.
//
// why: Scan reaches the hardware only through the ckcc executable, so the
// executable is the seam, and the production shape stays as it is.
func installCkccSubstitute(t *testing.T) func() string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the ckcc stand-in is a POSIX shell script")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "invocations")
	script := "#!/bin/sh\necho \"$@\" >> \"$CKCC_SUBSTITUTE_LOG\"\n"

	if err := os.WriteFile(filepath.Join(dir, "ckcc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write ckcc stand-in: %v", err)
	}

	t.Setenv("CKCC_SUBSTITUTE_LOG", logPath)
	t.Setenv("PATH", dir)

	return func() string {
		b, err := os.ReadFile(logPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read ckcc stand-in log: %v", err)
		}
		return string(b)
	}
}

// requireOneScanAction fails the test unless the op asked exactly once, about
// coldcard.ScanAction, naming the caller as the actor.
func requireOneScanAction(t *testing.T, authority *recordingAuth, caller *astral.Identity) {
	t.Helper()

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("coldcard.scan made %d authorization calls; want exactly 1", len(actions))
	}

	action, ok := actions[0].(*coldcard.ScanAction)
	if !ok {
		t.Fatalf("coldcard.scan named %q; want %q", actions[0].ObjectType(), (&coldcard.ScanAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("coldcard.scan named actor %v; want the caller %v", action.Actor(), caller)
	}
}

// TestColdcardScanRefusesCallerWithoutPermits is the coverage measure for
// coldcard.ScanAction: coldcard.scan must ask before it acts, and must reject
// when the answer is no.
//
// The module is a bare struct holding only the auth dependency. The ckcc
// stand-in records any device access, so "touches no device" is observed
// directly as well as through the byte count.
func TestColdcardScanRefusesCallerWithoutPermits(t *testing.T) {
	invocations := installCkccSubstitute(t)
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: false}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	err := route(t, mod.OpScan, caller, coldcard.MethodScan, w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("coldcard.scan answered a caller holding no permits: got err %v, want a rejection", err)
	}

	if n := w.written(); n != 0 {
		t.Fatalf("coldcard.scan wrote %d bytes to a refused caller; want none", n)
	}

	if got := invocations(); got != "" {
		t.Fatalf("coldcard.scan ran the device tool for a refused caller: %q", got)
	}

	requireOneScanAction(t, authority, caller)
}

// TestColdcardScanDispatchesAuthorizedCallerToScan shows the guard lets an
// authorized caller through to the scan: the device tool is asked to list the
// attached devices, and the caller receives the reply.
func TestColdcardScanDispatchesAuthorizedCallerToScan(t *testing.T) {
	invocations := installCkccSubstitute(t)
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	if err := route(t, mod.OpScan, caller, coldcard.MethodScan, w); err != nil {
		t.Fatalf("coldcard.scan refused an authorized caller: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("coldcard.scan did not finish answering an authorized caller")
	}

	if got := invocations(); got != "list\n" {
		t.Fatalf("coldcard.scan ran the device tool as %q; want one \"list\"", got)
	}

	if w.written() == 0 {
		t.Fatal("coldcard.scan sent an authorized caller no reply")
	}

	requireOneScanAction(t, authority, caller)
}
