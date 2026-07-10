package objects

import (
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"sync"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// Describe fans the query out to all registered describers in parallel and
// merges their descriptors onto one channel, closed when every describer is
// done or ctx is cancelled.
func (mod *Module) Describe(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	var results = make(chan *objects.Descriptor)

	go func() {
		defer close(results)

		var wg sync.WaitGroup

		for _, d := range mod.describers.Clone() {
			d := d
			wg.Add(1)
			go func() {
				defer wg.Done()
				_res, _err := d.DescribeObject(ctx, objectID)
				if _err != nil {
					return
				}

				for {
					descriptor, ok, err := sig.RecvOk(ctx, _res)
					if err != nil || !ok {
						return
					}

					if descriptor == nil {
						mod.log.Errorv(1, "describer %T returned nil descriptor", d)
						continue
					}

					if err := sig.Send(ctx, results, descriptor); err != nil {
						return
					}
				}
			}()
		}

		wg.Wait()
	}()

	return results, nil
}

// AddDescriber registers a describer, skipping it if one with the same source
// identity is already registered.
func (mod *Module) AddDescriber(describer objects.Describer) error {
	source, ok, err := objectsmod.SourceIdentity(describer)
	if err != nil {
		return err
	}
	if ok {
		mod.externalMu.Lock()
		defer mod.externalMu.Unlock()

		if containsSourceIdentity(&mod.describers, source) {
			return nil
		}
	}

	return mod.describers.Add(describer)
}
