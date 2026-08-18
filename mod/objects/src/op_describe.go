package objects

import (
	"slices"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

type opDescribeArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Out    string
	Zone   astral.Zone
	Only   *string
	Except *string
}

// OpDescribe streams an object's descriptors, filtered by the Only/Except type
// lists, and terminates the stream with an EOS marker.
func (mod *Module) OpDescribe(ctx *astral.Context, q *routing.IncomingQuery, args opDescribeArgs) (err error) {
	if !mod.authorizeSeeObjects(ctx, q, args.ID, "") {
		return q.Reject()
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).IncludeZone(args.Zone).WithTimeout(time.Minute)
	defer cancel()

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	descriptors, err := mod.Describe(ctx, args.ID)
	if err != nil {
		return
	}

	return streamDescriptors(ctx, ch, descriptors, args)
}

// streamDescriptors relays descriptors to ch, filtered by the Only/Except type lists,
// and terminates the stream with EOS. It is split out from OpDescribe so the filter can
// be tested without a live query/transport.
func streamDescriptors(ctx *astral.Context, ch *channel.Channel, descriptors <-chan *objects.Descriptor, args opDescribeArgs) error {
	var only, except []string
	if args.Only != nil && len(*args.Only) > 0 {
		only = strings.Split(*args.Only, ",")
	}
	if args.Except != nil && len(*args.Except) > 0 {
		except = strings.Split(*args.Except, ",")
	}

	for {
		descriptor, ok, recvErr := sig.RecvOk(ctx, descriptors)
		if recvErr != nil || !ok {
			break
		}

		// why: Descriptor.ObjectType() is the constant wrapper type
		// "mod.objects.describe_result", identical for every descriptor; the type a caller
		// filters on is the type of the described object carried in Data.
		// note: describers are a plugin extension point (Module.AddDescriber), so a nil
		// Data must not panic the loop; it matches no Only entry and no Except entry.
		var objectType string
		if descriptor.Data != nil {
			objectType = descriptor.Data.ObjectType()
		}

		if len(only) > 0 && !slices.Contains(only, objectType) {
			continue
		}

		if len(except) > 0 && slices.Contains(except, objectType) {
			continue
		}

		if err := ch.Send(descriptor); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
