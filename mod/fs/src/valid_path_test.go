package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astrald/mod/fs"
)

// TestValidPath covers every way a caller-supplied path is refused, and the two
// shapes that are accepted: a clean directory, and one that only cleans to a
// directory.
func TestValidPath(t *testing.T) {
	root := t.TempDir()

	file := filepath.Join(root, "regular")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		path    string
		want    string
		wantErr error
	}{
		{"relative", "relative/dir", "", fs.ErrNotAbsolute},
		{"empty", "", "", fs.ErrNotAbsolute},
		{"missing", filepath.Join(root, "absent"), "", os.ErrNotExist},
		{"regular file", file, "", fs.ErrNotDirectory},
		{"directory", nested, nested, nil},
		{"cleaned", filepath.Join(root, "nested", "..", "nested") + "/", nested, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validPath(tc.path)

			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("validPath(%q): unexpected error %v", tc.path, err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("validPath(%q): got error %v, want %v", tc.path, err, tc.wantErr)
			}

			if got != tc.want {
				t.Fatalf("validPath(%q): got %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
