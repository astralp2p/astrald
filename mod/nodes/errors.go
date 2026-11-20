package nodes

import "errors"

var (
	ErrInvalidEndpointFormat = errors.New("invalid endpoint format")
	ErrEndpointParse         = errors.New("endpoint parse failed")
	ErrIdentityResolve       = errors.New("identity resolve failed")
	ErrEndpointResolve       = errors.New("endpoint resolve failed")
	ErrExcessLink            = errors.New("excess link")
	ErrLinkNotProduced       = errors.New("link not produced")
	ErrInvalidSessionState   = errors.New("invalid session state")
	ErrMigrationNotSupported = errors.New("migration not supported")
	ErrSessionNotFound       = errors.New("session not found")
	ErrSessionClosed         = errors.New("session closed")
	ErrLinkNotFound          = errors.New("link not found")
	ErrBufferClosed          = errors.New("buffer closed")
	ErrBufferOverflow        = errors.New("buffer overflow")
	ErrSessionAlreadyOnLink  = errors.New("session already on link")
	ErrLinkDenied            = errors.New("link denied")
)
