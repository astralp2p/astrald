package paths

import (
	"errors"
	"testing"

	"github.com/cryptopunkscc/astral-go/astral/sig"
)

func TestPathUnder(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		root     string
		expected bool
	}{
		{
			name:     "path equals root",
			path:     "/home/user",
			root:     "/home/user",
			expected: true,
		},
		{
			name:     "path under root",
			path:     "/home/user/docs/file.txt",
			root:     "/home/user",
			expected: true,
		},
		{
			name:     "path not under root",
			path:     "/var/data/file.txt",
			root:     "/home/user",
			expected: false,
		},
		{
			name:     "path prefix but not under root",
			path:     "/home/username/file.txt",
			root:     "/home/user",
			expected: false,
		},
		{
			name:     "deeply nested path",
			path:     "/home/user/a/b/c/d/e/file.txt",
			root:     "/home/user",
			expected: true,
		},
		{
			name:     "root is separator only",
			path:     "/home/user/file.txt",
			root:     "/",
			expected: true,
		},
		{
			name:     "sibling directory",
			path:     "/home/other/file.txt",
			root:     "/home/user",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PathUnder(tt.path, tt.root, '/')
			if result != tt.expected {
				t.Errorf("PathUnder(%q, %q, '/') = %v, expected %v", tt.path, tt.root, result, tt.expected)
			}
		})
	}
}

func TestWidestRoots(t *testing.T) {
	tests := []struct {
		name     string
		roots    []string
		expected []string
	}{
		{
			name:     "empty",
			roots:    []string{},
			expected: nil,
		},
		{
			name:     "single root",
			roots:    []string{"/home/user"},
			expected: []string{"/home/user"},
		},
		{
			name:     "non-overlapping roots",
			roots:    []string{"/home/user", "/var/data"},
			expected: []string{"/home/user", "/var/data"},
		},
		{
			name:     "nested root filtered",
			roots:    []string{"/home/user", "/home/user/docs"},
			expected: []string{"/home/user"},
		},
		{
			name:     "deeply nested filtered",
			roots:    []string{"/home", "/home/user", "/home/user/docs"},
			expected: []string{"/home"},
		},
		{
			name:     "mixed overlapping and separate",
			roots:    []string{"/home/user", "/home/user/docs", "/var/data"},
			expected: []string{"/home/user", "/var/data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WidestRoots(tt.roots)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d roots, expected %d: got %v, expected %v", len(result), len(tt.expected), result, tt.expected)
				return
			}

			resultSet := sig.Set[string]{}
			for _, r := range result {
				resultSet.Add(r)
			}

			for _, exp := range tt.expected {
				if !resultSet.Contains(exp) {
					t.Errorf("expected root %q not found in result %v", exp, result)
				}
			}
		})
	}
}

func TestPathTrie_Covers(t *testing.T) {
	tests := []struct {
		name     string
		roots    []string
		path     string
		expected bool
	}{
		// Basic coverage
		{
			name:     "empty trie covers nothing",
			roots:    []string{},
			path:     "/home/user/file.txt",
			expected: false,
		},
		{
			name:     "exact match",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs",
			expected: true,
		},
		{
			name:     "path under root",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs/file.txt",
			expected: true,
		},
		{
			name:     "deeply nested under root",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs/a/b/c/file.txt",
			expected: true,
		},

		// Boundary conditions
		{
			name:     "path not under root",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/photos/pic.jpg",
			expected: false,
		},
		{
			name:     "path boundary - docs vs document",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/document/file.txt",
			expected: false,
		},
		{
			name:     "path boundary - user vs username",
			roots:    []string{"/home/user"},
			path:     "/home/username/file.txt",
			expected: false,
		},
		{
			name:     "sibling path not covered",
			roots:    []string{"/home/user/docs"},
			path:     "/home/other/file.txt",
			expected: false,
		},
		{
			name:     "parent path not covered by child root",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/file.txt",
			expected: false,
		},
		{
			name:     "cousin path not covered",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/photos/vacation/pic.jpg",
			expected: false,
		},

		// Multiple roots
		{
			name:     "multiple roots - first matches",
			roots:    []string{"/home/user/docs", "/home/user/photos"},
			path:     "/home/user/docs/file.txt",
			expected: true,
		},
		{
			name:     "multiple roots - second matches",
			roots:    []string{"/home/user/docs", "/home/user/photos"},
			path:     "/home/user/photos/pic.jpg",
			expected: true,
		},
		{
			name:     "multiple roots - none matches",
			roots:    []string{"/home/user/docs", "/home/user/photos"},
			path:     "/home/user/music/song.mp3",
			expected: false,
		},
		{
			name:     "nested roots - parent covers child path",
			roots:    []string{"/home/user", "/home/user/docs"},
			path:     "/home/user/docs/file.txt",
			expected: true,
		},

		// Filesystem root coverage
		{
			name:     "filesystem root covers any path",
			roots:    []string{"/"},
			path:     "/home/user/file.txt",
			expected: true,
		},
		{
			name:     "filesystem root covers deep path",
			roots:    []string{"/"},
			path:     "/a/b/c/d/e/f/g.txt",
			expected: true,
		},
		{
			name:     "filesystem root with other roots",
			roots:    []string{"/", "/home/user"},
			path:     "/var/log/syslog",
			expected: true,
		},

		// Normalization edge cases
		{
			name:     "trailing slash in root",
			roots:    []string{"/home/user/docs/"},
			path:     "/home/user/docs/file.txt",
			expected: true,
		},
		{
			name:     "trailing slash in path",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs/sub/",
			expected: true,
		},
		{
			name:     "dot-dot in path normalized",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs/../docs/file.txt",
			expected: true,
		},
		{
			name:     "dot-dot escapes root",
			roots:    []string{"/home/user/docs"},
			path:     "/home/user/docs/../photos/file.txt",
			expected: false,
		},
		{
			name:     "redundant slashes normalized",
			roots:    []string{"/home//user///docs"},
			path:     "/home/user/docs/file.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trie, err := NewPathTrie(tt.roots, '/')
			if err != nil {
				t.Fatalf("NewPathTrie failed: %v", err)
			}
			result, err := trie.Covers(tt.path)
			if err != nil {
				t.Fatalf("Covers failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("trie.Covers(%q) = %v, expected %v (roots: %v)",
					tt.path, result, tt.expected, tt.roots)
			}
		})
	}
}

func TestPathTrie_Insert(t *testing.T) {
	tests := []struct {
		name       string
		roots      []string
		checkPaths map[string]bool
	}{
		{
			name:  "single insertion",
			roots: []string{"/home/user/docs"},
			checkPaths: map[string]bool{
				"/home/user/docs":          true,
				"/home/user/docs/file.txt": true,
				"/home/user":               false,
				"/home/user/other":         false,
			},
		},
		{
			name:  "multiple insertions same branch",
			roots: []string{"/home/user/docs", "/home/user/docs/private"},
			checkPaths: map[string]bool{
				"/home/user/docs":                true,
				"/home/user/docs/file.txt":       true,
				"/home/user/docs/private":        true,
				"/home/user/docs/private/secret": true,
				"/home/user":                     false,
			},
		},
		{
			name:  "multiple insertions different branches",
			roots: []string{"/home/user/docs", "/var/data"},
			checkPaths: map[string]bool{
				"/home/user/docs/file.txt": true,
				"/var/data/file.txt":       true,
				"/home/user/other":         false,
				"/var/log":                 false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trie, err := NewPathTrie(tt.roots, '/')
			if err != nil {
				t.Fatalf("NewPathTrie failed: %v", err)
			}
			for path, expected := range tt.checkPaths {
				result, err := trie.Covers(path)
				if err != nil {
					t.Fatalf("Covers failed: %v", err)
				}
				if result != expected {
					t.Errorf("trie.Covers(%q) = %v, expected %v", path, result, expected)
				}
			}
		})
	}
}

func TestPathTrie_ReturnsErrorOnRelativePath(t *testing.T) {
	t.Run("NewPathTrie returns error on relative path", func(t *testing.T) {
		_, err := NewPathTrie([]string{"relative/path"}, '/')
		if !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("expected ErrNotAbsolute, got %v", err)
		}
	})

	t.Run("Covers returns error on relative path", func(t *testing.T) {
		trie, _ := NewPathTrie([]string{"/home/user"}, '/')
		_, err := trie.Covers("relative/path")
		if !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("expected ErrNotAbsolute, got %v", err)
		}
	})

	t.Run("dot path is relative", func(t *testing.T) {
		_, err := NewPathTrie([]string{"./relative"}, '/')
		if !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("expected ErrNotAbsolute, got %v", err)
		}
	})

	t.Run("dot-dot path is relative", func(t *testing.T) {
		_, err := NewPathTrie([]string{"../relative"}, '/')
		if !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("expected ErrNotAbsolute, got %v", err)
		}
	})
}
