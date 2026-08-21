package user

import "github.com/astralp2p/astrald/mod/scheduler"

// SyncNodesTask reconciles local node membership with a remote identity,
// exchanging swarm member lists and contracts.
type SyncNodesTask interface {
	scheduler.Task
}
