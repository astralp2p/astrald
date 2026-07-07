package services

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/api/services"
	"github.com/cryptopunkscc/astral-go/astral"
)

// DiscoverServices runs all registered ServiceDiscoverers with the provided options and merges
// their returned streams into a single output channel.
func (mod *Module) DiscoverServices(
	ctx *astral.Context,
	caller *astral.Identity,
	follow bool,
) (<-chan *services.Update, error) {
	var out = make(chan *services.Update)
	var sources []<-chan *services.Update

	// collect all sources for discovery
	for _, discoverer := range mod.discoverers.Clone() {
		source, err := discoverer.DiscoverServices(ctx, caller, follow)
		if err != nil {
			mod.log.Logv(2, "%v.DiscoverServices: %v", discoverer, err)
			continue
		}
		sources = append(sources, source)
	}

	go func() {
		defer close(out)

		// collect snapshots and send them to the output channel
		var wg sync.WaitGroup
		for _, source := range sources {
			wg.Add(1)
			go func(source <-chan *services.Update) {
				defer wg.Done()
				for update := range source {
					if update == nil {
						return
					}
					select {
					case <-ctx.Done():
						return
					case out <- update:
					}
				}
			}(source)
		}
		wg.Wait()

		// return if we only wanted the snapshot
		if !follow {
			return
		}

		// send the separator
		out <- nil

		// collect updates and send them to the output channel
		for _, source := range sources {
			wg.Add(1)
			go func(source <-chan *services.Update) {
				defer wg.Done()
				for update := range source {
					select {
					case <-ctx.Done():
						return
					case out <- update:
					}

				}
			}(source)
		}

		wg.Wait()
	}()

	return out, nil
}
