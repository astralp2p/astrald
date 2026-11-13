package vfs

import (
	"strings"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

// maxPathLen is the longest path in a request, in bytes.
const maxPathLen = 4096

// parsePath splits a path into its names. The root "/" has no names.
//
// A path starts with '/', has no empty segment and no trailing '/' except the root, and is at most 4,096 bytes.
// Every segment passes vfsapi.ValidateName. The error is vfsapi.ErrInvalidPath, and a path is never repaired.
func parsePath(path string) ([]string, error) {
	switch {
	case len(path) > maxPathLen:
		return nil, vfsapi.ErrInvalidPath
	case !strings.HasPrefix(path, "/"):
		return nil, vfsapi.ErrInvalidPath
	case path == "/":
		return nil, nil
	}

	names := strings.Split(path[1:], "/")
	for _, name := range names {
		if err := vfsapi.ValidateName(name); err != nil {
			return nil, err
		}
	}

	return names, nil
}
