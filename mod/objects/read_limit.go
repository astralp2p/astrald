package objects

import (
	"math"

	"github.com/astralp2p/astral-go/astral"
)

// ResolveReadLimit returns the byte count of the read window at offset in the object.
// objectID is the resolved full ID, never a partial request.
// A zero limit covers the rest of the object.
// An offset equal to the size covers 0 bytes.
// A size above math.MaxInt64, a negative offset or limit, or an offset past the end returns ErrOutOfBounds.
func ResolveReadLimit(objectID *astral.ObjectID, offset, limit int64) (int64, error) {
	if objectID.Size > math.MaxInt64 || offset < 0 || limit < 0 {
		return 0, ErrOutOfBounds
	}

	// why: offset and limit are untrusted, and their sum overflows int64 at offset 1, limit math.MaxInt64.
	remaining := int64(objectID.Size) - offset
	if remaining < 0 {
		return 0, ErrOutOfBounds
	}

	if limit == 0 {
		return remaining, nil
	}

	return min(limit, remaining), nil
}
