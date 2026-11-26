package user

import "github.com/astralp2p/astrald/mod/scheduler"

// SyncNodesTask reconciles local node membership with a remote identity,
// pulling its swarm state and pushing bans and app relay contracts.
type SyncNodesTask interface {
	scheduler.Task
}
