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

type objectFinderOps struct {
	finders []objects.Finder
}

type objectFindArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

// WithObjectFinder mounts the objects.find IPC op and registers the app as a Finder with the node.
// All provided finders are fanned out concurrently per find request.
func WithObjectFinder(finders ...objects.Finder) ServeOption {
	return func(cfg *serveConfig) error {
		if len(finders) == 0 {
			return errors.New("no object finders")
		}

		cfg.mounts = append(cfg.mounts, func(router astral.Router) error {
			adder, ok := router.(routing.ScopedOpRouter)
			if !ok {
				return fmt.Errorf("router %T cannot mount scoped object finder route", router)
			}

			op, err := routing.NewOp((&objectFinderOps{finders: finders}).Find)
			if err != nil {
				return err
			}

			return adder.AddScopedOp(objects.ModuleName, "find", op)
		})
		cfg.hooks = append(cfg.hooks, objectsClient.RegisterFinder)
		return nil
	}
}

func (ops *objectFinderOps) Find(ctx *astral.Context, q *routing.IncomingQuery, args objectFindArgs) error {
	// note: caller identity is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Finder

	// note: zone is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Finder

	// note: no timeout - relies on the caller's ctx (lib/apps does not enforce timeouts)
	ctx, cancel := ctx.WithCancel()
	defer cancel()

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.ID == nil || args.ID.IsZero() {
		if err := ch.Send(astral.NewError("id is required")); err != nil {
			return err
		}
		return ch.Send(&astral.EOS{})
	}

	providers := ops.find(ctx, args.ID)
	for {
		provider, ok, err := sig.RecvOk(ctx, providers)
		if err != nil || !ok {
			break
		}

		if err := ch.Send(provider); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

func (ops *objectFinderOps) find(ctx *astral.Context, id *astral.ObjectID) <-chan *astral.Identity {
	results := make(chan *astral.Identity)
	found := make(chan *astral.Identity)
	var wg sync.WaitGroup

	for _, finder := range ops.finders {
		finder := finder
		wg.Add(1)
		go func() {
			defer wg.Done()

			if finder == nil {
				return
			}

			providers, err := finder.FindObject(ctx, id)
			if err != nil || providers == nil {
				return
			}

			for {
				provider, ok, err := sig.RecvOk(ctx, providers)
				if err != nil || !ok {
					return
				}

				if provider == nil || provider.IsZero() {
					continue
				}

				if err := sig.Send(ctx, found, provider); err != nil {
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
		for {
			provider, ok, err := sig.RecvOk(ctx, found)
			if err != nil || !ok {
				return
			}
			if err := sig.Send(ctx, results, provider); err != nil {
				return
			}
		}
	}()

	return results
}
