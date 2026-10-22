package coldcard

import (
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/coldcard"
)

// scanInterval is the interval the loop tests configure.
//
// why short: a test observes that at least three scans ran and never an exact
// number, so the value only has to fall inside the test's wait.
const scanInterval = 5 * time.Millisecond

// startRun runs mod until the returned cancel is called, and reports Run's
// return.
func startRun(t *testing.T, mod *Module) (cancel func(), done <-chan error) {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	out := make(chan error, 1)
	go func() { out <- mod.Run(ctx) }()

	return cancel, out
}

// awaitScans waits until the module has scanned n times, so no test races the
// loop it started.
//
// why the long wait: a test's own read of the signal bounds it, not the
// interval, so a missing scan fails in seconds rather than hanging.
func awaitScans(t *testing.T, backend *fakeBackend, n int) {
	t.Helper()

	for range n {
		select {
		case <-backend.scans:
		case <-time.After(10 * time.Second):
			t.Fatalf("the module scanned %d times; want at least %d", backend.listCount(), n)
		}
	}
}

// awaitReturn waits for Run to return nil after its context ended.
func awaitReturn(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v; want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// awaitDevices waits until the recorded set holds want.
func awaitDevices(t *testing.T, mod *Module, want map[string]string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !maps.Equal(mod.devices.Clone(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("recorded devices = %v; want %v", mod.devices.Clone(), want)
		}

		time.Sleep(time.Millisecond)
	}
}

// entrySink records the level and the closing text of every log entry.
//
// why no synchronization: LogEntry runs synchronously under the root mutex.
type entrySink struct {
	levels []uint8
	texts  []string
}

func (s *entrySink) LogEntry(e *log.Entry) {
	s.levels = append(s.levels, uint8(e.Level))
	s.texts = append(s.texts, astral.Stringify(e.Objects[len(e.Objects)-1]))
}

// holdsAt reports whether one entry at level carries text.
func (s *entrySink) holdsAt(level uint8, text string) bool {
	for i, want := range s.levels {
		if want == level && strings.Contains(s.texts[i], text) {
			return true
		}
	}

	return false
}

// newLoopModule returns a module on backend that scans at scanInterval.
func newLoopModule(backend *fakeBackend) *Module {
	return &Module{
		log:     quietLog(),
		config:  Config{ScanInterval: scanInterval},
		backend: backend,
	}
}

// quietLog returns a logger that prints nothing.
func quietLog() *log.Logger {
	l := log.New(nil)
	// why: the filter gates the console writer alone; subscribers still receive
	// every entry, so this keeps the pump from printing during the test.
	l.SetFilter(func(*log.Entry) bool { return false })

	return l
}

// captureLog returns a sink holding every entry mod's logger emits.
func captureLog(mod *Module) *entrySink {
	sink := &entrySink{}
	mod.log.AddLogger(sink)

	return sink
}

// TestRunScansRepeatedlyUntilContextEnds: the module scans at start and again on
// each tick, and returns once its context ends.
func TestRunScansRepeatedlyUntilContextEnds(t *testing.T) {
	backend := newFakeBackend()
	cancel, done := startRun(t, newLoopModule(backend))

	awaitScans(t, backend, 3)

	cancel()
	awaitReturn(t, done)
}

// TestRunScansAtStart: the first scan runs before any tick, so a device attached
// at start is a signer without waiting out the interval.
func TestRunScansAtStart(t *testing.T) {
	backend := newFakeBackend()
	backend.setSerials("AAA111")

	mod := newLoopModule(backend)
	// why: an interval longer than the test's wait proves the observed scan is
	// the one Run runs at start, not a tick.
	mod.config.ScanInterval = time.Hour

	cancel, done := startRun(t, mod)

	awaitScans(t, backend, 1)

	awaitDevices(t, mod, map[string]string{"AAA111": "02abcdef"})

	cancel()
	awaitReturn(t, done)
}

// TestRunKeepsScanningAfterFailedScan: a scan that fails is logged at level 1
// and the loop retries on the next tick rather than ending.
func TestRunKeepsScanningAfterFailedScan(t *testing.T) {
	backend := newFakeBackend()
	backend.setListErr(errors.New("device bus unavailable"))

	mod := newLoopModule(backend)
	sink := captureLog(mod)

	cancel, done := startRun(t, mod)

	awaitScans(t, backend, 3)

	cancel()
	awaitReturn(t, done)

	if !sink.holdsAt(1, "device bus unavailable") {
		t.Fatalf("log entries = %v; want one at level 1 naming the failed scan", sink.texts)
	}
}

// TestRunDiscoversDevicePluggedInAfterStart: the recorded set follows the bus,
// so a device that appears after start becomes a signer without coldcard.scan.
func TestRunDiscoversDevicePluggedInAfterStart(t *testing.T) {
	backend := newFakeBackend()

	mod := newLoopModule(backend)
	cancel, done := startRun(t, mod)

	awaitScans(t, backend, 1)
	backend.setSerials("AAA111")

	awaitDevices(t, mod, map[string]string{"AAA111": "02abcdef"})

	cancel()
	awaitReturn(t, done)
}

// TestRunForgetsDeviceUnpluggedAfterStart: a device unplugged after start stops
// being offered as a signer on the next tick.
func TestRunForgetsDeviceUnpluggedAfterStart(t *testing.T) {
	backend := newFakeBackend()
	backend.setSerials("AAA111")

	mod := newLoopModule(backend)
	cancel, done := startRun(t, mod)

	awaitDevices(t, mod, map[string]string{"AAA111": "02abcdef"})

	backend.setSerials()

	awaitDevices(t, mod, map[string]string{})

	cancel()
	awaitReturn(t, done)
}

// TestScanUsesBackend: Scan reads the devices through the module's backend, at
// the Coldcard path.
func TestScanUsesBackend(t *testing.T) {
	backend := newFakeBackend()
	backend.setSerials("AAA111", "BBB222")

	mod := &Module{log: log.New(nil), backend: backend}

	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan err = %v; want nil", err)
	}

	want := map[string]string{"AAA111": "02abcdef", "BBB222": "02abcdef"}
	if got := mod.devices.Clone(); !maps.Equal(got, want) {
		t.Fatalf("recorded devices = %v; want %v", got, want)
	}

	wantLookups := []string{"AAA111 " + coldcard.BIP44Path, "BBB222 " + coldcard.BIP44Path}
	if got := backend.keyLookups(); !slices.Equal(got, wantLookups) {
		t.Fatalf("backend pubkey reads = %q; want %q", got, wantLookups)
	}
}

// TestSignerSignsThroughBackend: the signer Engine hands out signs through the
// same backend the scan recorded the device with.
func TestSignerSignsThroughBackend(t *testing.T) {
	backend := newFakeBackend()
	backend.setSerials("AAA111")

	mod := &Module{log: log.New(nil), backend: backend}

	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan err = %v; want nil", err)
	}

	signer, err := (&Engine{mod: mod}).NewTextSigner(publicKey(t, "02abcdef"), crypto.SchemeBIP137)
	if err != nil {
		t.Fatalf("NewTextSigner err = %v; want nil", err)
	}

	if _, err = signer.SignText(nil, "hello"); err != nil {
		t.Fatalf("SignText err = %v; want nil", err)
	}

	want := []string{"AAA111 " + coldcard.BIP44Path + " hello"}
	if got := backend.messages(); !slices.Equal(got, want) {
		t.Fatalf("backend signatures = %q; want %q", got, want)
	}
}

// publicKey returns the secp256k1 public key a hex key names.
func publicKey(t *testing.T, keyHex string) *crypto.PublicKey {
	t.Helper()

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key %q: %v", keyHex, err)
	}

	return &crypto.PublicKey{Type: "secp256k1", Key: key}
}
