package apps

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/objects"
	objectsClient "github.com/cryptopunkscc/astrald/mod/objects/client"
)

type objectDescriberOps struct {
	describers []objects.Describer
}

type objectDescribeArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

// WithObjectDescriber mounts the objects.describe IPC op and registers the app as a Describer with the node.
// All provided describers are fanned out concurrently per describe request.
func WithObjectDescriber(describers ...objects.Describer) ServeOption {
	return func(cfg *serveConfig) error {
		if len(describers) == 0 {
			return errors.New("no object describers")
		}

		cfg.mounts = append(cfg.mounts, func(router astral.Router) error {
			adder, ok := router.(routing.ScopedOpRouter)
			if !ok {
				return fmt.Errorf("router %T cannot mount scoped object describer route", router)
			}

			op, err := routing.NewOp((&objectDescriberOps{describers: describers}).Describe)
			if err != nil {
				return err
			}

			return adder.AddScopedOp(objects.ModuleName, "describe", op)
		})
		cfg.hooks = append(cfg.hooks, objectsClient.RegisterDescriber)
		return nil
	}
}

func (ops *objectDescriberOps) Describe(ctx *astral.Context, q *routing.IncomingQuery, args objectDescribeArgs) error {
	// note: caller identity is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Describer

	// note: zone is not propagated; mount a custom op to access it. Maybe in the future we will modify design of interface object.Describer

	// note: no timeout - relies on the caller's ctx (lib/apps does not enforce timeouts)

	// note: descriptor.SourceID is left to the impl; the node-side ExternalDescriber stamps it later. Revisit if app-side stamping is wanted.
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

	descriptors := ops.describe(ctx, args.ID)
	for {
		descriptor, ok, err := sig.RecvOk(ctx, descriptors)
		if err != nil || !ok {
			break
		}

		if err := ch.Send(descriptor); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

func (ops *objectDescriberOps) describe(ctx *astral.Context, id *astral.ObjectID) <-chan *objects.Descriptor {
	results := make(chan *objects.Descriptor)
	found := make(chan *objects.Descriptor)
	var wg sync.WaitGroup

	for _, describer := range ops.describers {
		describer := describer
		wg.Add(1)
		go func() {
			defer wg.Done()

			if describer == nil {
				return
			}

			stream, err := describer.DescribeObject(ctx, id)
			if err != nil || stream == nil {
				return
			}

			for {
				descriptor, ok, err := sig.RecvOk(ctx, stream)
				if err != nil || !ok {
					return
				}

				if descriptor == nil || descriptor.Data == nil {
					continue
				}

				if err := sig.Send(ctx, found, descriptor); err != nil {
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
			descriptor, ok, err := sig.RecvOk(ctx, found)
			if err != nil || !ok {
				return
			}
			if err := sig.Send(ctx, results, descriptor); err != nil {
				return
			}
		}
	}()

	return results
}
