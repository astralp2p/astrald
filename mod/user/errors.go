package user

import "errors"

// ErrSwarmEntryHeld is returned by SwarmState.AddSwarmEntry for an entry that
// CheckSwarmEntry holds (EntryWait): it is not stored yet, and a later merge
// may admit it.
var ErrSwarmEntryHeld = errors.New("swarm entry held until what it needs arrives")

// ErrSwarmEntryDropped is wrapped by the error SwarmState.AddSwarmEntry returns
// for an entry CheckSwarmEntry drops (EntryDrop).
var ErrSwarmEntryDropped = errors.New("swarm entry dropped")
