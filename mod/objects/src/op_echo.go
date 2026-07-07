package objects

import (
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opEchoArgs struct {
	Only   *string `query:"optional"` // only echo these object types (comma separated)
	Except *string `query:"optional"` // do not echo these object types (comma separated)
	Stop   string  `query:"optional"` // close the channel when this object type is received (like EOS)
	Strict bool    `query:"optional"` // fail-fast on objects whose blueprint isn't registered (probes wire-schema understanding)
	In     string  `query:"optional"`
	Out    string  `query:"optional"`
}

// OpEcho relays received objects back, optionally filtered by Only/Except and
// stopped on the Stop type. Strict mode fails on objects whose blueprint isn't
// registered; lenient mode passes them through unparsed.
func (mod *Module) OpEcho(ctx *astral.Context, q *routing.IncomingQuery, args opEchoArgs) (err error) {
	// why: Strict drops the AllowUnparsed fallback so a missing blueprint surfaces as a
	// decode error instead of silently re-emitting the original bytes. The default keeps
	// pass-through behavior used by relay/debug callers.
	opts := []channel.ConfigFunc{channel.WithFormats(args.In, args.Out)}
	if !args.Strict {
		opts = append(opts, channel.AllowUnparsed(true))
	}
	ch := channel.New(q.AcceptRaw(), opts...)
	defer ch.Close()

	return echo(ch, args)
}

// echo runs the receive/relay loop over an established channel. It is split out from
// OpEcho so the loop can be tested without a live query/transport.
func echo(ch *channel.Channel, args opEchoArgs) error {
	// prepare lists
	var only, except []string
	if args.Only != nil && len(*args.Only) > 0 {
		only = strings.Split(*args.Only, ",")
	}
	if args.Except != nil && len(*args.Except) > 0 {
		except = strings.Split(*args.Except, ",")
	}

	var stop = len(args.Stop) > 0

	for {
		object, err := ch.Receive()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, astral.ErrStreamCorrupted):
			// why: the non-binary receivers (canonical/json/text) have no per-object framing,
			// so once an unknown type desyncs the stream they latch this error and return it on
			// EVERY subsequent Receive without touching the reader. The ErrBlueprintNotFound case
			// below would `continue` on it forever.
			if args.Strict {
				return ch.Send(astral.NewError(err.Error()))
			}
			return nil
		case errors.Is(err, astral.ErrBlueprintNotFound):
			// why: in strict mode an unparseable object is a verification failure — surface
			// it in-band and end the stream. Lenient mode keeps skipping so one missing
			// blueprint doesn't kill an otherwise-useful relay.
			if args.Strict {
				return ch.Send(astral.NewError(err.Error()))
			}
			continue
		default:
			return err
		}

		if stop && object.ObjectType() == args.Stop {
			return nil
		}

		if len(only) > 0 {
			if !slices.Contains(only, object.ObjectType()) {
				continue
			}
		}

		if len(except) > 0 {
			if slices.Contains(except, object.ObjectType()) {
				continue
			}
		}

		err = ch.Send(object)
		if err != nil {
			return err
		}
	}
}
