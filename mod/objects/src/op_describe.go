package objects

import (
	"slices"
	"strings"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astral-go/sig"
)

type opDescribeArgs struct {
	ID     *astral.ObjectID
	Out    string      `query:"optional"`
	Zone   astral.Zone `query:"optional"`
	Only   *string     `query:"optional"`
	Except *string     `query:"optional"`
}

// OpDescribe streams an object's descriptors, filtered by the Only/Except type
// lists, and terminates the stream with an EOS marker.
func (mod *Module) OpDescribe(ctx *astral.Context, q *routing.IncomingQuery, args opDescribeArgs) (err error) {
	ctx, cancel := ctx.WithIdentity(q.Caller()).IncludeZone(args.Zone).WithTimeout(time.Minute)
	defer cancel()

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var only, except []string
	if args.Only != nil && len(*args.Only) > 0 {
		only = strings.Split(*args.Only, ",")
	}
	if args.Except != nil && len(*args.Except) > 0 {
		except = strings.Split(*args.Except, ",")
	}

	descriptors, err := mod.Describe(ctx, args.ID)
	if err != nil {
		return
	}

	for {
		descriptor, ok, recvErr := sig.RecvOk(ctx, descriptors)
		if recvErr != nil || !ok {
			break
		}

		if len(only) > 0 && !slices.Contains(only, descriptor.ObjectType()) {
			continue
		}

		if len(except) > 0 && slices.Contains(except, descriptor.ObjectType()) {
			continue
		}

		err = ch.Send(descriptor)
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
