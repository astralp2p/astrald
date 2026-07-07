package archives

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/mod/archives"
	"gorm.io/gorm"
	"sync"
)

const zipMimeType = "application/zip"

var _ archives.Module = &Module{}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	db     *gorm.DB

	mu            sync.Mutex
	autoIndexZone astral.Zone
}

// Run parses the AutoIndexZones config string into a Zone bitmask used to
// filter which network zones trigger automatic archive indexing.
func (mod *Module) Run(ctx *astral.Context) error {
	mod.autoIndexZone = astral.Zones(mod.config.AutoIndexZones)

	return nil
}

func (mod *Module) String() string {
	return archives.ModuleName
}
