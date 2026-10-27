package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const unixScheme = "unix:"

// FromFlag expands "~/", makes s absolute against the working directory, and cleans it.
func FromFlag(s string) (string, error) {
	s, err := expandHome(s)
	if err != nil {
		return "", err
	}

	return filepath.Abs(s)
}

// FromEnv returns s cleaned, or false when s is empty or relative.
//
// why: the XDG base directory spec makes a relative value invalid and ignored.
func FromEnv(s string) (string, bool) {
	if !filepath.IsAbs(s) {
		return "", false
	}

	return filepath.Clean(s), true
}

// FromConfig expands "~/" and cleans s; a relative s is ErrNotAbsolute.
func FromConfig(s string) (string, error) {
	s, err := expandHome(s)
	if err != nil {
		return "", err
	}

	if err = CheckAbs(s); err != nil {
		return "", err
	}

	return filepath.Clean(s), nil
}

// FromConfigAddr applies FromConfig to the path of a "unix:" IPC address. Any other address is
// returned unchanged.
func FromConfigAddr(addr string) (string, error) {
	p, ok := strings.CutPrefix(addr, unixScheme)
	if !ok {
		return addr, nil
	}

	p, err := FromConfig(p)
	if err != nil {
		return "", err
	}

	return unixScheme + p, nil
}

// CheckAbs returns ErrNotAbsolute, naming p, unless p is absolute.
func CheckAbs(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%q: %w", p, ErrNotAbsolute)
	}

	return nil
}

// CheckAddr returns ErrNotAbsolute, naming the path, when addr is a "unix:" IPC address with a
// relative path. Any other address passes.
func CheckAddr(addr string) error {
	p, ok := strings.CutPrefix(addr, unixScheme)
	if !ok {
		return nil
	}

	return CheckAbs(p)
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(s string) (string, error) {
	rest, ok := strings.CutPrefix(s, "~/")
	if !ok {
		return s, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: %w", s, err)
	}

	return filepath.Join(home, rest), nil
}
