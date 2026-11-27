package nodes

import "github.com/astralp2p/astrald/mod/scheduler"
import (
	"github.com/astralp2p/astral-go/api/nodes"
)

// LinkProducerTask is a scheduler task whose Result is valid only after the task completes.
type LinkProducerTask interface {
	scheduler.Task
	Result() (info *nodes.LinkInfo, err error)
}

type EnsureLinkTask interface {
	LinkProducerTask
}

type CreateLinkTask interface {
	LinkProducerTask
}

type CleanupEndpointsTask interface {
	scheduler.Task
}

// MaintainLinkTask keeps an active link to a remote peer alive; restarts the
// link when it drops.
type MaintainLinkTask interface {
	scheduler.Task
	// Wake makes the task link to its peer now when the peer is not linked,
	// also while the task waits out its backoff.
	Wake()
}
