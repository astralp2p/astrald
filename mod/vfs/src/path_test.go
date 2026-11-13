package vfs

import (
	"errors"
	"slices"
	"strings"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestParsePathValid(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/", nil},
		{"/a", []string{"a"}},
		{"/a/b/c", []string{"a", "b", "c"}},
		{"/a.txt", []string{"a.txt"}},
		{"/.hidden/..x/x..", []string{".hidden", "..x", "x.."}},
		{"/é/è", []string{"é", "è"}},
		{"/" + strings.Repeat("a", 255), []string{strings.Repeat("a", 255)}},
		{"/ a /b ", []string{" a ", "b "}},
	}

	for _, tt := range tests {
		got, err := parsePath(tt.path)
		if err != nil {
			t.Errorf("parsePath(%q): %v", tt.path, err)
			continue
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("parsePath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestParsePathInvalid(t *testing.T) {
	tests := map[string]string{
		"empty":                 "",
		"relative":              "a/b",
		"dot relative":          "./a",
		"empty segment":         "/a//b",
		"double root":           "//",
		"trailing slash":        "/a/",
		"dot segment":           "/a/./b",
		"dotdot segment":        "/a/../b",
		"dot":                   "/.",
		"nul":                   "/a\x00b",
		"invalid utf8":          "/a\xffb",
		"name of 256 bytes":     "/" + strings.Repeat("a", 256),
		"path of 4097 bytes":    "/" + strings.Repeat("a/", 2048) + "a",
		"path of 4097, no tail": strings.Repeat("/a", 2048) + "b",
	}

	for name, path := range tests {
		if _, err := parsePath(path); !errors.Is(err, vfsapi.ErrInvalidPath) {
			t.Errorf("%s: parsePath(len %d) error = %v, want invalid path", name, len(path), err)
		}
	}
}

func TestParsePathLengthLimit(t *testing.T) {
	// 2048 segments of one byte plus separators is 4096 bytes: the longest valid path.
	longest := strings.Repeat("/a", 2048)
	if len(longest) != maxPathLen {
		t.Fatalf("test path is %d bytes, want %d", len(longest), maxPathLen)
	}

	names, err := parsePath(longest)
	if err != nil {
		t.Fatalf("parsePath at the limit: %v", err)
	}
	if len(names) != 2048 {
		t.Fatalf("got %d names, want 2048", len(names))
	}

	if _, err := parsePath(longest + "/a"); !errors.Is(err, vfsapi.ErrInvalidPath) {
		t.Fatalf("parsePath over the limit: %v, want invalid path", err)
	}
}
