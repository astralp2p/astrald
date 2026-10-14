package objects

import (
	"github.com/astralp2p/astrald/core"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// Describe fans the query out to all registered describers in parallel, unless
// describe.SourcesOnly is set, and asks each of describe.Sources when the
// context zone permits the network. It merges their descriptors onto one
// channel, closed when every describer and source is done or ctx is cancelled.
func (mod *Module) Describe(ctx *astral.Context, describe *objects.Describe) (<-chan *objects.Descriptor, error) {
	objectID := describe.ObjectID

	var results = make(chan *objects.Descriptor)

	go func() {
		defer close(results)

		var wg sync.WaitGroup

		now := time.Now()

		var describers []objects.Describer
		if !describe.SourcesOnly {
			describers = mod.describers.Clone()
		}

		for _, d := range describers {
			d := d

			// why: waiting for a registrant that has gone is the cost the lease exists
			// to avoid, and it would be paid on every describe falling between the
			// expiry and the next sweep tick.
			if expiredProvider(d, now) {
				continue
			}

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

		// ask the sources
		if ctx.Zone().Is(astral.ZoneNetwork) {
			for _, nodeID := range describe.Sources {
				nodeID := nodeID

				// why: asking this node would re-enter objects.describe as a local query and run the describers again.
				if nodeID.IsEqual(mod.node.Identity()) {
					continue
				}

				wg.Add(1)
				go func() {
					defer wg.Done()

					// why: the host authorized its caller; the source serves the host as
					// itself and trusts that decision (objects.describe spec).
					// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
					_res, errPtr := objectscli.New(nodeID, core.NewClientAs(mod.node, mod.node.Identity())).Describe(ctx.WithIdentity(mod.node.Identity()), objectID)
					if _res == nil {
						if errPtr != nil && *errPtr != nil {
							mod.log.Errorv(1, "describe %v: %v", nodeID, *errPtr)
						}
						return
					}

					for {
						descriptor, ok, err := sig.RecvOk(ctx, _res)
						if err != nil {
							return
						}
						if !ok {
							if errPtr != nil && *errPtr != nil {
								mod.log.Errorv(1, "describe %v: %v", nodeID, *errPtr)
							}
							return
						}

						if descriptor == nil {
							mod.log.Errorv(1, "network describe %v returned nil descriptor", nodeID)
							continue
						}

						if err := sig.Send(ctx, results, descriptor); err != nil {
							return
						}
					}
				}()
			}
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

// removeExternalDescriber unregisters an external describer and logs the removal.
func (mod *Module) removeExternalDescriber(describer *ExternalDescriber) {
	// note: a concurrent call can remove the same describer first.
	if err := mod.describers.Remove(describer); err != nil {
		return
	}

	mod.log.Logv(1, "removed external describer %v", describer.id)
}
