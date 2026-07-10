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
