package user

import "github.com/astralp2p/astrald/mod/scheduler"

// SyncNodesAction reconciles local node membership with a remote identity,
// exchanging swarm member lists and contracts.
type SyncNodesAction interface {
	scheduler.Task
}
