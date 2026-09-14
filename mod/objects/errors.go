package objects

import "errors"

var (
	ErrNotFound       = errors.New("object not found")
	ErrObjectTooLarge = errors.New("object too large")
	ErrOutOfBounds    = errors.New("offset or limit out of bounds")
	ErrNoSpaceLeft    = errors.New("no space left on device")
	ErrClosedPipe     = errors.New("pipe closed")
	ErrPushRejected   = errors.New("push rejected")

	ErrNilSourceIdentifier   = errors.New("source identifier is nil")
	ErrInvalidSourceIdentity = errors.New("source identity is invalid")

	ErrExternalRegistrationFromNetwork = errors.New("external discoverer registration cannot come from the network")
	ErrExternalRegistrationSelf        = errors.New("node identity cannot register as an external discoverer")
	ErrExternalNotAuthorized           = errors.New("external discoverer is not authorized to serve objects")
)
