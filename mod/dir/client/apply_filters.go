package dir

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/dir"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func ApplyFilters(ctx *astral.Context, identity *astral.Identity, filters ...string) (bool, error) {
	return Default().ApplyFilters(ctx, identity, filters...)
}

// ApplyFilters tests identity against the named server-side filters and returns
// true if the identity matches all of them.
func (client *Client) ApplyFilters(ctx *astral.Context, identity *astral.Identity, filters ...string) (bool, error) {
	ch, err := client.queryCh(ctx, dir.MethodSetAlias, query.Args{
		"id":      identity,
		"filters": strings.Join(filters, ","),
	})
	if err != nil {
		return false, err
	}

	var match *astral.Bool
	err = ch.Switch(channel.Expect(&match), channel.PassErrors)
	if err != nil {
		return false, err
	}

	return (bool)(*match), nil
}
