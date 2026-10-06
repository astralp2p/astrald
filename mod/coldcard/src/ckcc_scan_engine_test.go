package coldcard

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/coldcard"
	"github.com/astralp2p/astrald/mod/coldcard/ckcc"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

const standInList = `printf '\nColdcard AAA111:\n{"fingerprint": "x"}\n\nColdcard BBB222:\n{}\n'`

// why: installCkccSubstitute lists no device; these tests need per-test device behavior on the same seam.
// note: caseBody runs inside `case "$*" in ... esac` with PATH holding only the stand-in, so it uses shell builtins.
func installCkccStandIn(t *testing.T, caseBody string) func() string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the ckcc stand-in is a POSIX shell script")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "invocations")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$CKCC_STAND_IN_LOG\"\n" +
		"case \"$*\" in\n" + caseBody + "\nesac\n"

	if err := os.WriteFile(filepath.Join(dir, "ckcc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write ckcc stand-in: %v", err)
	}

	t.Setenv("CKCC_STAND_IN_LOG", logPath)
	t.Setenv("PATH", dir)

	return func() string {
		b, err := os.ReadFile(logPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read ckcc stand-in log: %v", err)
		}
		return string(b)
	}
}

func TestCkccListParsesSerials(t *testing.T) {
	installCkccStandIn(t, "list) "+standInList+" ;;")

	devices, err := ckcc.List()
	if err != nil {
		t.Fatalf("List err = %v; want nil", err)
	}

	var serials []string
	for _, dev := range devices {
		serials = append(serials, dev.Serial)
	}

	if got, want := strings.Join(serials, " "), "AAA111 BBB222"; got != want {
		t.Fatalf("List serials = [%s]; want [%s]", got, want)
	}
}

func TestScanRecordsDevicesWithReadablePubKey(t *testing.T) {
	invocations := installCkccStandIn(t, strings.Join([]string{
		"list) " + standInList + " ;;",
		"'-s AAA111 pubkey '*) printf '02abcdef\\n' ;;",
		"'-s BBB222 pubkey '*) exit 1 ;;",
	}, "\n"))

	mod := &Module{log: log.New(nil)}

	if err := mod.Scan(); err != nil {
		t.Fatalf("Scan err = %v; want nil", err)
	}

	got := mod.devices.Clone()
	if len(got) != 1 || got["AAA111"] != "02abcdef" {
		t.Fatalf("devices = %v; want map[AAA111:02abcdef]", got)
	}

	if want := "-s AAA111 pubkey " + coldcard.BIP44Path + "\n"; !strings.Contains(invocations(), want) {
		t.Fatalf("ckcc invocations = %q; want a line %q", invocations(), want)
	}
}

func TestDeviceForPublicKeyHex(t *testing.T) {
	mod := &Module{}
	mod.devices.Set("AAA111", "02abcdef")

	if dev := mod.deviceForPublicKeyHex("02abcdef"); dev == nil || dev.Serial != "AAA111" {
		t.Fatalf("deviceForPublicKeyHex(02abcdef) = %+v; want serial AAA111", dev)
	}

	if dev := mod.deviceForPublicKeyHex("ff"); dev != nil {
		t.Fatalf("deviceForPublicKeyHex(ff) = %+v; want nil", dev)
	}
}

func TestEngineNewTextSigner(t *testing.T) {
	mod := &Module{}
	mod.devices.Set("AAA111", "02abcdef")
	engine := &Engine{mod: mod}

	known, err := hex.DecodeString("02abcdef")
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}

	signer, err := engine.NewTextSigner(&crypto.PublicKey{Type: "secp256k1", Key: known}, crypto.SchemeBIP137)
	if err != nil {
		t.Fatalf("NewTextSigner(known key) err = %v; want nil", err)
	}

	ms, ok := signer.(*MessageSigner)
	if !ok {
		t.Fatalf("NewTextSigner(known key) = %T; want *MessageSigner", signer)
	}

	if ms.dev == nil || ms.dev.Serial != "AAA111" || ms.path != coldcard.BIP44Path {
		t.Fatalf("MessageSigner = device %+v, path %q; want serial AAA111, path %q", ms.dev, ms.path, coldcard.BIP44Path)
	}

	tests := []struct {
		name   string
		key    *crypto.PublicKey
		scheme string
		want   error
	}{
		{"unknown key", &crypto.PublicKey{Type: "secp256k1", Key: []byte{0xff}}, crypto.SchemeBIP137, cryptomod.ErrUnsupported},
		{"asn1 scheme", &crypto.PublicKey{Type: "secp256k1", Key: known}, crypto.SchemeASN1, cryptomod.ErrUnsupportedScheme},
		{"foreign key type", &crypto.PublicKey{Type: "ed25519", Key: known}, crypto.SchemeBIP137, cryptomod.ErrUnsupportedKeyType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := engine.NewTextSigner(tt.key, tt.scheme)
			if signer != nil || !errors.Is(err, tt.want) {
				t.Fatalf("NewTextSigner = %v, %v; want nil, %v", signer, err, tt.want)
			}
		})
	}
}

func TestMessageSignerSignText(t *testing.T) {
	invocations := installCkccStandIn(t, "'-s AAA111 msg '*) printf 'AQID\\n' ;;")
	signer := &MessageSigner{dev: ckcc.NewDevice("AAA111"), path: coldcard.BIP44Path}

	sig, err := signer.SignText(nil, "[abc] hello")
	if err != nil {
		t.Fatalf("SignText err = %v; want nil", err)
	}

	if sig.Scheme != crypto.SchemeBIP137 || !bytes.Equal(sig.Data, []byte{1, 2, 3}) {
		t.Fatalf("SignText = scheme %q, data %x; want scheme %q, data 010203", sig.Scheme, []byte(sig.Data), crypto.SchemeBIP137)
	}

	if got, want := invocations(), "-s AAA111 msg -p "+coldcard.BIP44Path+" -j [abc] hello\n"; got != want {
		t.Fatalf("ckcc invocations = %q; want %q", got, want)
	}
}

func TestMessageSignerSignTextDeviceErrors(t *testing.T) {
	signer := &MessageSigner{dev: ckcc.NewDevice("AAA111"), path: coldcard.BIP44Path}

	t.Run("device error", func(t *testing.T) {
		installCkccStandIn(t, "*) printf 'device locked\\n' >&2; exit 1 ;;")

		_, err := signer.SignText(nil, "hello")
		if err == nil || err.Error() != "device locked" {
			t.Fatalf("SignText err = %v; want %q", err, "device locked")
		}
	})

	t.Run("malformed base64", func(t *testing.T) {
		installCkccStandIn(t, "*) printf '!!notbase64\\n' ;;")

		_, err := signer.SignText(nil, "hello")

		var corrupt base64.CorruptInputError
		if !errors.As(err, &corrupt) {
			t.Fatalf("SignText err = %v; want a base64.CorruptInputError", err)
		}
	})
}

func TestDevicePubKeyDefaultsToBIP44Path(t *testing.T) {
	invocations := installCkccStandIn(t, "*) printf '02abcdef\\n' ;;")

	got, err := ckcc.NewDevice("AAA111").PubKey("")
	if err != nil || got != "02abcdef" {
		t.Fatalf("PubKey(\"\") = %q, %v; want 02abcdef, nil", got, err)
	}

	if gotLog, want := invocations(), "-s AAA111 pubkey "+coldcard.BIP44Path+"\n"; gotLog != want {
		t.Fatalf("ckcc invocations = %q; want %q", gotLog, want)
	}
}
