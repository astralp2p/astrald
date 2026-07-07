package gateway

import (
	"github.com/cryptopunkscc/astral-go/api/services"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/gateway"
	servicesmod "github.com/cryptopunkscc/astrald/mod/services"
)

var _ servicesmod.Discoverer = &Module{}

// DiscoverServices advertises the gateway service when enabled.
// If follow is true, the channel stays open (with a nil sentinel after the initial update) until ctx is done.
func (mod *Module) DiscoverServices(
	ctx *astral.Context,
	caller *astral.Identity,
	follow bool,
) (<-chan *services.Update, error) {
	var ch = make(chan *services.Update, 2)

	if mod.config.Gateway.Enabled {
		ch <- &services.Update{
			Available:  true,
			Name:       gateway.ModuleName,
			ProviderID: mod.node.Identity(),
		}
	}

	if !follow {
		close(ch)
		return ch, nil
	}

	ch <- nil

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}
