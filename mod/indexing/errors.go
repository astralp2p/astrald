package indexing

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/astral"
)

var ErrObjectAlreadyAdded = errors.New("object already added")
var ErrObjectNotPresent = errors.New("object not present")
var ErrIndexNotFound = errors.New("index not found")
var ErrRepoAlreadySyncing = errors.New("repo already syncing")
var ErrRepoNotSyncing = errors.New("repo not syncing")
var ErrRepositoryNotFound = errors.New("repository not found")
var ErrAckMismatch = errors.New("ack does not match delivered change")
var ErrInvalidIndexHeight = errors.New("index height must advance by exactly 1")
var ErrIndexingTemporarilyFailed = astral.NewError("indexing temporarily failed")

// IsIndexingTemporarilyFailed reports whether err is a temporary indexing failure.
// why: uses string comparison because astral.Error does not support errors.Is unwrapping.
func IsIndexingTemporarilyFailed(err error) bool {
	return err != nil && err.Error() == ErrIndexingTemporarilyFailed.Error()
}
