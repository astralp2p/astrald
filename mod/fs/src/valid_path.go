package fs

import (
	"os"
	"path/filepath"

	"github.com/astralp2p/astrald/mod/fs"
)

// validPath checks a caller-supplied repository path and returns it cleaned. The
// path must be absolute, and it must name an existing directory.
//
// why: this is not a security control — a grant is what decides who may attach
// at all. It moves the failure to the attach. A relative path is stored verbatim
// and resolved against the daemon's working directory by every later
// filepath.Join, so what it names depends on how astrald was started. A path
// that is missing or is not a directory still registers a repository and still
// joins the local write group; the break then surfaces elsewhere, as an empty
// Scan and a failing Create.
func validPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fs.ErrNotAbsolute
	}

	clean := filepath.Clean(path)

	stat, err := os.Stat(clean)
	switch {
	case err != nil:
		return "", err
	case !stat.IsDir():
		return "", fs.ErrNotDirectory
	}

	return clean, nil
}
