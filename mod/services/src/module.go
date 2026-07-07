package services

import (
	servicescli "github.com/cryptopunkscc/astral-go/api/services/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/services"
)

const ModuleName = "services"

type Module struct {
	Deps

	node   astral.Node
	log    *log.Logger
	router routing.OpRouter
	db     *DB

	discoverers sig.Set[services.Discoverer]
}

var _ services.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) syncServices(ctx *astral.Context, providerID *astral.Identity, follow bool) error {
	client := servicescli.New(providerID, nil)

	ch, err := client.Discover(ctx, follow)
	if err != nil {
		return err
	}

	// clear cache
	err = mod.db.deleteAllProviderServices(providerID)
	if err != nil {
		return err
	}

	// process updates
	for update := range ch {
		switch {
		case update == nil:
			continue
		case update.Available:
			err = mod.db.createProviderService(update.ProviderID, string(update.Name), update.Info)
		default:
			err = mod.db.deleteProviderService(update.ProviderID, string(update.Name))
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (mod *Module) AddDiscoverer(discoverer services.Discoverer) error {
	return mod.discoverers.Add(discoverer)
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return ModuleName
}
