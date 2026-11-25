package nodes

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astrald/mod/nodes/frames"
)

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
	ErrWriteStalled          = errors.New("link write stalled")
	ErrLinkRetired           = errors.New("link retired")
)

// ErrSwarmEntryHeld is returned by SwarmState.AddSwarmEntry for an entry that
// CheckSwarmEntry holds (EntryWait): it is not stored yet, and a later merge
// may admit it.
var ErrSwarmEntryHeld = errors.New("swarm entry held until what it needs arrives")

// ErrSwarmEntryDropped is wrapped by the error SwarmState.AddSwarmEntry returns
// for an entry CheckSwarmEntry drops (EntryDrop).
var ErrSwarmEntryDropped = errors.New("swarm entry dropped")

// ErrPeerClosed is a link's cause of death when the peer announced its close
// with a close frame; Reason is the frame's reason.
type ErrPeerClosed struct {
	Reason frames.CloseReason
}

func (e *ErrPeerClosed) Error() string {
	return fmt.Sprintf("peer closed the link (%s)", e.Reason)
}
