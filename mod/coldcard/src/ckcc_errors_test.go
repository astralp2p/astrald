package coldcard

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/coldcard/ckcc"
)

// installCkccFailing puts a stand-in on PATH that fails on every invocation with
// the CLI's standard error.
func installCkccFailing(t *testing.T) {
	t.Helper()

	installCkccStandIn(t, "*) printf 'device locked\\n' >&2; exit 1 ;;")
}

// TestCkccListReportsStderr: a locked or missing device names its reason, which
// the CLI prints on standard error and an exit status alone never carries.
func TestCkccListReportsStderr(t *testing.T) {
	installCkccFailing(t)

	_, err := ckcc.List()
	if err == nil {
		t.Fatal("List err = nil; want the CLI's failure")
	}

	if !strings.Contains(err.Error(), "device locked") {
		t.Fatalf("List err = %q; want it to carry %q", err, "device locked")
	}
}

// TestCkccPubKeyReportsStderr is the same measure for a single-device read.
func TestCkccPubKeyReportsStderr(t *testing.T) {
	installCkccFailing(t)

	_, err := ckcc.NewDevice("AAA111").PubKey("")
	if err == nil {
		t.Fatal("PubKey err = nil; want the CLI's failure")
	}

	if !strings.Contains(err.Error(), "device locked") {
		t.Fatalf("PubKey err = %q; want it to carry %q", err, "device locked")
	}
}

// TestScanReportsStderr: the reason reaches the caller of Scan, which is what
// coldcard.scan answers with.
func TestScanReportsStderr(t *testing.T) {
	installCkccFailing(t)

	mod := &Module{log: log.New(nil), backend: ckccBackend{}}

	err := mod.Scan()
	if err == nil || !strings.Contains(err.Error(), "device locked") {
		t.Fatalf("Scan err = %v; want it to carry %q", err, "device locked")
	}
}

// TestCkccMissingExecutableIsNotFound: a machine with no ckcc installed answers
// exec.ErrNotFound through the wrap, not an opaque exit status.
func TestCkccMissingExecutableIsNotFound(t *testing.T) {
	// why: an empty directory on PATH lists no ckcc executable, and the test
	// needs no stand-in to say so.
	t.Setenv("PATH", t.TempDir())

	_, err := ckcc.List()
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("List err = %v; want one wrapping exec.ErrNotFound", err)
	}
}
