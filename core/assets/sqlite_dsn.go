package assets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/astralp2p/astrald/lib/paths"
)

// errQueryInPath reports a database path that holds a '?'.
var errQueryInPath = errors.New("database path contains '?'")

// dbDSN returns the DSN of the sqlite file at path: the path, '?', and dbSettings.
//
// why a plain path and not a file: URL: SQLite reads the first segment of a
// relative path in a file: URL as an authority and refuses it, and both drivers
// pass a string without the file: prefix to SQLite as an ordinary filename.
// note: both drivers cut the DSN at the first '?', so a path holding one is refused.
func dbDSN(path string) (string, error) {
	if err := paths.CheckAbs(path); err != nil {
		return "", err
	}

	if strings.Contains(path, "?") {
		return "", fmt.Errorf("%q: %w", path, errQueryInPath)
	}

	return path + "?" + dbSettings, nil
}
