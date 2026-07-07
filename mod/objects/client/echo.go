package objects

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// EchoOptions configures objects.echo. All fields are optional.
type EchoOptions struct {
	Only   []string // only echo these object types
	Except []string // do not echo these object types
	Stop   string   // close the channel when this object type is received
	Strict bool     // fail-fast on objects whose blueprint isn't registered
	In     string   // input format
	Out    string   // output format
}

// Echo opens an objects.echo channel. The caller drives sends and receives and
// must Close the returned channel.
func (client *Client) Echo(ctx *astral.Context, opts EchoOptions) (*channel.Channel, error) {
	args := query.Args{}
	if len(opts.Only) > 0 {
		args["only"] = strings.Join(opts.Only, ",")
	}
	if len(opts.Except) > 0 {
		args["except"] = strings.Join(opts.Except, ",")
	}
	if opts.Stop != "" {
		args["stop"] = opts.Stop
	}
	if opts.Strict {
		args["strict"] = true
	}
	if opts.In != "" {
		args["in"] = opts.In
	}
	if opts.Out != "" {
		args["out"] = opts.Out
	}

	return client.queryCh(ctx, objects.MethodEcho, args)
}

func Echo(ctx *astral.Context, opts EchoOptions) (*channel.Channel, error) {
	return Default().Echo(ctx, opts)
}
