package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/objects"

	_ "github.com/cryptopunkscc/astrald/mod/all/pub"
)

// Describe streams descriptors on the returned channel until EOS, then closes it.
// The error pointer is only valid once the channel is closed.
func (client *Client) Describe(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, *error) {
	ch, err := client.queryCh(ctx, objects.MethodDescribe, query.Args{
		"id": objectID.String(),
	})
	if err != nil {
		return nil, &err
	}

	out := make(chan *objects.Descriptor)
	errPtr := new(error)

	go func() {
		defer close(out)
		defer ch.Close()

		*errPtr = ch.Switch(
			func(descriptor *objects.Descriptor) error {
				return sig.Send(ctx, out, descriptor)
			},
			channel.BreakOnEOS,
			channel.PassErrors,
			channel.WithContext(ctx),
		)
	}()

	return out, errPtr
}

func Describe(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, *error) {
	return Default().Describe(ctx, objectID)
}
