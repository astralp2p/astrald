package auth

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
)

func TestResolveTokenFilesExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configs := []ExternalConfig{{TokenFile: "~/token"}, {TokenValue: "v"}}
	if err := resolveTokenFiles(configs); err != nil {
		t.Fatalf("resolveTokenFiles: %v", err)
	}
	if want := filepath.Join(home, "token"); configs[0].TokenFile != want {
		t.Fatalf("token_file = %q, want %q", configs[0].TokenFile, want)
	}
	if configs[1].TokenFile != "" {
		t.Fatalf("unset token_file became %q", configs[1].TokenFile)
	}
}

func TestResolveTokenFilesRefusesRelativePath(t *testing.T) {
	configs := []ExternalConfig{{TokenValue: "v"}, {TokenFile: "token"}}

	err := resolveTokenFiles(configs)
	if !errors.Is(err, paths.ErrNotAbsolute) || !strings.Contains(err.Error(), "auth.yaml external_authorizers[1].token_file") {
		t.Fatalf("resolveTokenFiles(relative) error = %v, want ErrNotAbsolute naming auth.yaml external_authorizers[1].token_file", err)
	}
}
