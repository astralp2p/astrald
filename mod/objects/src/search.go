package objects

import (
	objectsmod "github.com/cryptopunkscc/astrald/mod/objects"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	objectscli "github.com/cryptopunkscc/astrald/mod/objects/client"
)

// Search runs all local searchers, and network searchers when the context zone permits, merging their results into one channel.
// The channel closes once every searcher finishes; the returned error reports only setup failures.
func (mod *Module) Search(ctx *astral.Context, query objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	search := &objects.Search{
		CallerID: ctx.Identity(),
		Query:    query,
	}

	for _, pre := range mod.searchPre.Clone() {
		pre.PreprocessSearch(search)
	}

	var results = make(chan *objects.SearchResult)
	var wg sync.WaitGroup

	// run local searchers
	for _, searcher := range mod.searchers.Clone() {
		searcher := searcher
		wg.Add(1)
		go func() {
			defer wg.Done()

			_res, _err := searcher.SearchObject(ctx, query)
			if _err != nil {
				return
			}

			for {
				result, ok, err := sig.RecvOk(ctx, _res)
				if err != nil || !ok {
					return
				}

				if result == nil || result.ObjectID == nil || result.ObjectID.IsZero() {
					mod.log.Errorv(1, "searcher %T returned invalid result", searcher)
					continue
				}

				if err := sig.Send(ctx, results, result); err != nil {
					return
				}
			}
		}()
	}

	// run network searchers
	if ctx.Zone().Is(astral.ZoneNetwork) {
		for _, nodeID := range search.Sources {
			nodeID := nodeID

			wg.Add(1)
			go func() {
				defer wg.Done()

				// execute search
				_results, errPtr := objectscli.New(nodeID, astrald.Default()).Search(ctx, query)
				if _results == nil {
					if errPtr != nil && *errPtr != nil {
						mod.log.Errorv(1, "search %v: %v", nodeID, *errPtr)
					}
					return
				}

				// copy results
				for {
					result, ok, err := sig.RecvOk(ctx, _results)
					if err != nil {
						return
					}
					if !ok {
						if errPtr != nil && *errPtr != nil {
							mod.log.Errorv(1, "search %v: %v", nodeID, *errPtr)
						}
						return
					}

					if result == nil || result.ObjectID == nil || result.ObjectID.IsZero() {
						mod.log.Errorv(1, "network search %v returned invalid result", nodeID)
						continue
					}

					if err := sig.Send(ctx, results, result); err != nil {
						return
					}
				}
			}()
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results, nil
}

// AddSearcher registers a searcher, deduplicating by source identity so each source is added at most once.
func (mod *Module) AddSearcher(searcher objects.Searcher) error {
	source, ok, err := objectsmod.SourceIdentity(searcher)
	if err != nil {
		return err
	}
	if ok {
		mod.externalMu.Lock()
		defer mod.externalMu.Unlock()

		if containsSourceIdentity(&mod.searchers, source) {
			return nil
		}
	}

	return mod.searchers.Add(searcher)
}
