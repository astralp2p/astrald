package user

import "errors"

// ErrSwarmEntryHeld is returned by the sync merge for an entry the entry check
// holds (user.EntryWait): it is not stored yet, and a later merge may admit
// it. A typed write of SwarmState counts a held entry as written.
var ErrSwarmEntryHeld = errors.New("swarm entry held until what it needs arrives")

// ErrSwarmEntryDropped is wrapped by the error a SwarmState write returns for
// an entry the entry check drops (user.EntryDrop).
var ErrSwarmEntryDropped = errors.New("swarm entry dropped")

// ErrNotAllowed is returned by a SwarmState write this node may not make, for
// example an Approval of a request its tier may not answer.
var ErrNotAllowed = errors.New("this node may not write that swarm entry")
