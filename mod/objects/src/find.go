package objects

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// Find fans the query out to all registered finders in parallel and merges the
// provider identities they return onto one channel, closed once every finder is
// done or ctx is cancelled.
func (mod *Module) Find(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *astral.Identity, error) {
	finders := mod.finders.Clone()
	results := make(chan *astral.Identity)
	var wg sync.WaitGroup

	for _, finder := range finders {
		finder := finder
		wg.Add(1)
		go func() {
			defer wg.Done()

			providers, err := finder.FindObject(ctx, objectID)
			if err != nil {
				return
			}

			for {
				provider, ok, err := sig.RecvOk(ctx, providers)
				if err != nil || !ok {
					return
				}

				if provider == nil || provider.IsZero() {
					mod.log.Errorv(1, "finder %T returned invalid provider", finder)
					continue
				}

				if err := sig.Send(ctx, results, provider); err != nil {
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results, nil
}

// AddFinder registers a finder, skipping it if one with the same source
// identity is already registered.
func (mod *Module) AddFinder(finder objects.Finder) error {
	source, ok, err := objects.SourceIdentity(finder)
	if err != nil {
		return err
	}

	if ok {
		mod.externalMu.Lock()
		defer mod.externalMu.Unlock()

		if containsSourceIdentity(&mod.finders, source) {
			return nil
		}
	}

	return mod.finders.Add(finder)
}
