package objects

import (
	"slices"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

type opDescribeArgs struct {
	ID          *astral.ObjectID `query:"required"`
	Out         string
	Zone        *astral.Zone
	Only        *string
	Except      *string
	Sources     string // comma-separated identities to ask
	SourcesOnly bool   // ask only Sources, not the registered describers
}

// OpDescribe streams an object's descriptors, filtered by the Only/Except type
// lists, and terminates the stream with an EOS marker.
func (mod *Module) OpDescribe(ctx *astral.Context, q *routing.IncomingQuery, args opDescribeArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	}) {
		return q.Reject()
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).WithTimeout(time.Minute)
	defer cancel()

	if args.Zone != nil {
		ctx = ctx.LimitZone(*args.Zone)
	}

	// why: a query from the network answers from this node alone. Its sender
	// already fans out, and a second hop sends it back (objects.describe spec).
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// resolve sources if provided
	var sources []*astral.Identity
	if len(args.Sources) > 0 {
		for _, name := range strings.Split(args.Sources, ",") {
			source, err := mod.Dir.ResolveIdentity(name)
			if err != nil {
				return ch.Send(astral.Err(err))
			}
			if !slices.ContainsFunc(sources, source.IsEqual) {
				sources = append(sources, source)
			}
		}
	}

	descriptors, err := mod.Describe(ctx, &objects.Describe{
		CallerID:    q.Caller(),
		ObjectID:    args.ID,
		Sources:     sources,
		SourcesOnly: args.SourcesOnly,
	})
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
