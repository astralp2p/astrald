package services

import (
	"errors"
	"io"

	"github.com/cryptopunkscc/astral-go/api/services"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

type Client struct {
	astral   *astrald.Client
	targetID *astral.Identity
}

func New(targetID *astral.Identity, astral *astrald.Client) *Client {
	if astral == nil {
		astral = astrald.Default()
	}
	return &Client{astral: astral, targetID: targetID}
}

var defaultClient *Client

func Default() *Client {
	if defaultClient == nil {
		defaultClient = New(nil, astrald.Default())
	}
	return defaultClient
}

// Discover streams service updates from the target node. The channel first delivers the current
// snapshot; when follow is true a nil sentinel marks the snapshot/live boundary, followed by
// live updates until ctx is cancelled. When follow is false the channel closes after the snapshot.
func (client *Client) Discover(ctx *astral.Context, follow bool) (<-chan *services.Update, error) {
	ch, err := client.queryCh(ctx, services.MethodDiscover, query.Args{
		"follow": follow,
	})
	if err != nil {
		return nil, err
	}

	var out = make(chan *services.Update)

	go func() {
		defer ch.Close()
		defer close(out)

		// read the snapshot until EOS
		err := ch.Collect(func(object astral.Object) error {
			switch obj := object.(type) {
			case *services.Update:
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- obj:
				}
				return nil

			case *astral.EOS:
				return io.EOF

			default:
				return astral.NewErrUnexpectedObject(object)
			}
		})
		switch {
		case err == nil:
			return
		case errors.Is(err, io.EOF):
		default:
			return
		}

		if !follow {
			return
		}

		// send the separator
		select {
		case <-ctx.Done():
			return
		case out <- nil:
		}

		// handle updates
		ch.Handle(ctx, func(object astral.Object) {
			switch obj := object.(type) {
			case *services.Update:
				select {
				case <-ctx.Done():
					ch.Close()
				case out <- obj:
				}

			default:
				ch.Close()
			}
		})
	}()

	return out, nil
}

func Discover(ctx *astral.Context, follow bool) (<-chan *services.Update, error) {
	return Default().Discover(ctx, follow)
}

func (client *Client) queryCh(ctx *astral.Context, method string, args any, cfg ...channel.ConfigFunc) (*channel.Channel, error) {
	return client.astral.WithTarget(client.targetID).QueryChannel(ctx, method, args, cfg...)
}
