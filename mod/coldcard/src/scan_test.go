package coldcard

import (
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral/log"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// installCkccScript puts a stand-in for the ckcc device tool alone on PATH and
// returns the directory that drives it: `list` prints the file `list`, and a
// pubkey read prints the file `pub.<serial>`. A missing file makes the
// invocation fail, which is how a lost device and a lost bus are expressed.
//
// why: Scan reaches the hardware only through the ckcc executable, so the
// executable is the seam, and the production shape stays as it is.
func installCkccScript(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the ckcc stand-in is a POSIX shell script")
	}

	dir := t.TempDir()
	// why the explicit PATH: the test puts the stand-in alone on PATH, so the
	// script cannot find cat unless it restores one.
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\ncase \"$1\" in\nlist) cat \"$CKCC_DIR/list\" ;;\n-s) cat \"$CKCC_DIR/pub.$2\" ;;\nesac\n"

	if err := os.WriteFile(filepath.Join(dir, "ckcc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write ckcc stand-in: %v", err)
	}

	t.Setenv("CKCC_DIR", dir)
	t.Setenv("PATH", dir)

	return dir
}

// writeCkccFile writes one of the stand-in's driving files.
func writeCkccFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// removeCkccFile drops a driving file, so the stand-in's cat fails.
func removeCkccFile(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
}

func newScanModule() *Module {
	return &Module{log: log.New(nil)}
}

// TestScanForgetsUnlistedDevice: an unplugged ColdCard stops being offered as a
// signer, which is what Engine.NewTextSigner's doc comment promises.
func TestScanForgetsUnlistedDevice(t *testing.T) {
	dir := installCkccScript(t)
	writeCkccFile(t, dir, "list", "Coldcard AAA111:\n")
	writeCkccFile(t, dir, "pub.AAA111", "02abcdef\n")

	mod := newScanModule()
	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan with the device attached: %v", err)
	}
	if got, want := mod.devices.Clone(), map[string]string{"AAA111": "02abcdef"}; !maps.Equal(got, want) {
		t.Fatalf("devices after the first scan = %v; want %v", got, want)
	}

	writeCkccFile(t, dir, "list", "")

	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan with the device unplugged: %v", err)
	}
	if got := mod.devices.Clone(); len(got) != 0 {
		t.Errorf("devices after the device was unplugged = %v; want empty", got)
	}

	// the reported harm, in the units Engine.NewTextSigner promises
	key, err := hex.DecodeString("02abcdef")
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	signer, err := (&Engine{mod: mod}).NewTextSigner(
		&crypto.PublicKey{Type: "secp256k1", Key: key},
		crypto.SchemeBIP137,
	)
	if signer != nil {
		t.Errorf("NewTextSigner returned a signer for an unplugged device")
	}
	if !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Errorf("NewTextSigner err = %v; want ErrUnsupported", err)
	}
}

// TestScanRefreshesChangedPubKey: sig.Map.Set is insert-only, so a still-
// connected serial whose pubkey changed was never updated either.
func TestScanRefreshesChangedPubKey(t *testing.T) {
	dir := installCkccScript(t)
	writeCkccFile(t, dir, "list", "Coldcard AAA111:\n")
	writeCkccFile(t, dir, "pub.AAA111", "02abcdef\n")

	mod := newScanModule()
	if err := mod.Scan(); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	writeCkccFile(t, dir, "pub.AAA111", "03fedcba\n")

	if err := mod.Scan(); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if got := mod.devices.Clone()["AAA111"]; got != "03fedcba" {
		t.Errorf("recorded pubkey after it changed = %q; want 03fedcba", got)
	}
}

// TestScanKeepsDeviceWithUnreadablePubKey: the enumeration still lists the
// device as connected, so it keeps what was recorded for it.
func TestScanKeepsDeviceWithUnreadablePubKey(t *testing.T) {
	dir := installCkccScript(t)
	writeCkccFile(t, dir, "list", "Coldcard AAA111:\n")
	writeCkccFile(t, dir, "pub.AAA111", "02abcdef\n")

	mod := newScanModule()
	if err := mod.Scan(); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	removeCkccFile(t, dir, "pub.AAA111")

	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan with an unreadable pubkey: %v", err)
	}
	if got, want := mod.devices.Clone(), map[string]string{"AAA111": "02abcdef"}; !maps.Equal(got, want) {
		t.Errorf("devices after an unreadable pubkey = %v; want %v", got, want)
	}
}

// TestScanKeepsDevicesWhenListFails: a transient bus failure must never
// un-register every signer.
func TestScanKeepsDevicesWhenListFails(t *testing.T) {
	dir := installCkccScript(t)
	writeCkccFile(t, dir, "list", "Coldcard AAA111:\n")
	writeCkccFile(t, dir, "pub.AAA111", "02abcdef\n")

	mod := newScanModule()
	if err := mod.Scan(); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	removeCkccFile(t, dir, "list")

	if err := mod.Scan(); err == nil {
		t.Error("Scan returned nil when the enumeration failed")
	}
	if got, want := mod.devices.Clone(), map[string]string{"AAA111": "02abcdef"}; !maps.Equal(got, want) {
		t.Errorf("devices after a failed enumeration = %v; want %v", got, want)
	}
}
