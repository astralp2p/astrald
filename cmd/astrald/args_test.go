package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNodeRootsUnderRoot(t *testing.T) {
	cfg, data, err := nodeRoots("/n")
	if err != nil || cfg != "/n/config" || data != "/n/data" {
		t.Fatalf("nodeRoots(/n) = (%q, %q, %v), want (/n/config, /n/data, nil)", cfg, data, err)
	}
}

func TestNodeRootsIgnoreRelativeXDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG directories apply on Linux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relcfg")
	t.Setenv("XDG_DATA_HOME", "reldata")

	cfg, data, err := nodeRoots("")
	if err != nil {
		t.Fatalf("nodeRoots: %v", err)
	}
	if want := filepath.Join(home, ".config", nodeDirName); cfg != want {
		t.Errorf("config root = %q, want %q", cfg, want)
	}
	if want := filepath.Join(home, ".local", "share", nodeDirName); data != want {
		t.Errorf("data root = %q, want %q", data, want)
	}
}

func TestNodeRootsUseAbsoluteXDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG directories apply on Linux")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	t.Setenv("XDG_DATA_HOME", "/x/data")

	cfg, data, err := nodeRoots("")
	if err != nil || cfg != "/x/cfg/astrald" || data != "/x/data/astrald" {
		t.Fatalf("nodeRoots = (%q, %q, %v), want (/x/cfg/astrald, /x/data/astrald, nil)", cfg, data, err)
	}
}

func TestNodeRootsWithoutAbsoluteHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG directories apply on Linux")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	for _, home := range []string{"", "relhome"} {
		t.Setenv("HOME", home)
		_, _, err := nodeRoots("")
		if err == nil || !strings.Contains(err.Error(), "$XDG_CONFIG_HOME nor $HOME") {
			t.Errorf("nodeRoots with HOME=%q error = %v, want one naming $XDG_CONFIG_HOME and $HOME", home, err)
		}
	}
}
