package apps

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/routing"
	objectsClient "github.com/cryptopunkscc/astrald/mod/objects/client"
)

type objectSearcherOps struct {
	searchers []objects.Searcher
}

type objectSearchArgs struct {
	Query string `query:"key:q"`
	Out   string `query:"optional"`
}

// WithObjectSearcher mounts the objects.search IPC op and registers the app as a Searcher with the node.
// All provided searchers are fanned out concurrently per search request.
func WithObjectSearcher(searchers ...objects.Searcher) ServeOption {
	return func(cfg *serveConfig) error {
		if len(searchers) == 0 {
			return errors.New("no object searchers")
		}

		cfg.mounts = append(cfg.mounts, func(router astral.Router) error {
			adder, ok := router.(routing.ScopedOpRouter)
			if !ok {
				return fmt.Errorf("router %T cannot mount scoped object searcher route", router)
			}

			op, err := routing.NewOp((&objectSearcherOps{searchers: searchers}).Search)
			if err != nil {
				return err
			}

			return adder.AddScopedOp(objects.ModuleName, "search", op)
		})
		cfg.hooks = append(cfg.hooks, objectsClient.RegisterSearcher)
		return nil
	}
}

func (ops *objectSearcherOps) Search(ctx *astral.Context, q *routing.IncomingQuery, args objectSearchArgs) error {
	// note: caller identity is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Searcher
	// note: zone is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Searcher
	// note: no timeout - relies on the caller's ctx (lib/apps does not enforce timeouts)
	// note: empty Query is allowed; passed through as SearchQuery{} ("match everything"), matching node-side semantics.

	ctx, cancel := ctx.WithCancel()
	defer cancel()

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var searchQuery objects.SearchQuery
	_ = searchQuery.UnmarshalText([]byte(args.Query))

	results := ops.search(ctx, searchQuery)
	for {
		result, ok, err := sig.RecvOk(ctx, results)
		if err != nil || !ok {
			break
		}

		if err := ch.Send(result); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

func (ops *objectSearcherOps) search(ctx *astral.Context, query objects.SearchQuery) <-chan *objects.SearchResult {
	results := make(chan *objects.SearchResult)
	found := make(chan *objects.SearchResult)
	var wg sync.WaitGroup

	for _, searcher := range ops.searchers {
		searcher := searcher
		wg.Add(1)
		go func() {
			defer wg.Done()

			if searcher == nil {
				return
			}

			stream, err := searcher.SearchObject(ctx, query)
			if err != nil || stream == nil {
				return
			}

			for {
				result, ok, err := sig.RecvOk(ctx, stream)
				if err != nil || !ok {
					return
				}

				if result == nil || result.ObjectID == nil || result.ObjectID.IsZero() {
					continue
				}

				if err := sig.Send(ctx, found, result); err != nil {
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(found)
	}()

	go func() {
		defer close(results)
		// note: no dedup; the node-side objects.search op dedups across all searchers anyway.
		for {
			result, ok, err := sig.RecvOk(ctx, found)
			if err != nil || !ok {
				return
			}
			if err := sig.Send(ctx, results, result); err != nil {
				return
			}
		}
	}()

	return results
}
