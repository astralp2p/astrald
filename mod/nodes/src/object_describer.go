package nodes

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/api/objects"
	objectscli "github.com/cryptopunkscc/astral-go/api/objects/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
)

// DescribeObject fans out to every provider returned by FindObject and merges their
// descriptors into the result channel. Filtered providers are skipped. Requires the
// network zone. The channel closes once all providers are exhausted.
func (mod *Module) DescribeObject(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	var results = make(chan *objects.Descriptor)

	go func() {
		defer close(results)

		providers, err := mod.FindObject(ctx, objectID)
		if err != nil {
			return
		}

		var wg sync.WaitGroup

		for {
			providerID, ok, err := sig.RecvOk(ctx, providers)
			if err != nil || !ok {
				break
			}

			if mod.Dir.ApplyFilters(providerID, ctx.Filters()...) {
				continue
			}

			providerIDCopy := providerID

			wg.Add(1)
			go func() {
				defer wg.Done()

				_results, err := objectscli.New(providerIDCopy, nil).Describe(ctx, objectID)
				if *err != nil {
					return
				}

				for {
					result, ok, err := sig.RecvOk(ctx, _results)
					if err != nil || !ok {
						return
					}

					if err := sig.Send(ctx, results, result); err != nil {
						return
					}
				}
			}()
		}

		wg.Wait()
	}()

	return results, nil
}
