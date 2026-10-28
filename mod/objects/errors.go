package objects

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound       = errors.New("object not found")
	ErrObjectTooLarge = errors.New("object too large")
	ErrOutOfBounds    = errors.New("offset or limit out of bounds")
	ErrNoSpaceLeft    = errors.New("no space left on device")
	ErrClosedPipe     = errors.New("pipe closed")
	ErrPushRejected   = errors.New("push rejected")

	// ErrHashLookupUnsupported is returned by a repository that cannot find an object by a partial ID.
	// note: callers match this sentinel, not errors.ErrUnsupported, which a read-only Delete also returns.
	// why: a repository never returns it for a full ID, because purge skips an ErrUnsupported from Delete.
	ErrHashLookupUnsupported = fmt.Errorf("hash lookup: %w", errors.ErrUnsupported)

	// ErrAmbiguousObjectID is returned when two distinct stored full IDs share the hash of a partial ID.
	ErrAmbiguousObjectID = errors.New("ambiguous object id")

	ErrNilSourceIdentifier   = errors.New("source identifier is nil")
	ErrInvalidSourceIdentity = errors.New("source identity is invalid")

	ErrExternalRegistrationFromNetwork = errors.New("external discoverer registration cannot come from the network")
	ErrExternalRegistrationSelf        = errors.New("node identity cannot register as an external discoverer")
	ErrExternalNotAuthorized           = errors.New("external discoverer is not authorized to serve objects")
)
