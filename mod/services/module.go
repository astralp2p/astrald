package services

import "github.com/astralp2p/astral-go/astral"
import (
	"github.com/astralp2p/astral-go/api/services"
)

const ModuleName = "services"
const DBPrefix = "services__"

// Module is the registry and aggregate discoverer: it accepts Discoverer
// registrations and itself satisfies Discoverer by fanning out to all registered sources.
type Module interface {
	AddDiscoverer(Discoverer) error
	Discoverer
}

// Discoverer streams service availability updates to the caller.
// When follow is false the channel closes after the initial snapshot; when true it
// remains open and delivers incremental updates until ctx is cancelled.
type Discoverer interface {
	DiscoverServices(ctx *astral.Context, caller *astral.Identity, follow bool) (<-chan *services.Update, error)
}
