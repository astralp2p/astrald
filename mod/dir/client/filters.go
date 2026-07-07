package dir

import (
	"github.com/cryptopunkscc/astral-go/api/dir"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

func Filters(ctx *astral.Context) ([]string, error) {
	return Default().Filters(ctx)
}

func (client *Client) Filters(ctx *astral.Context) (filters []string, err error) {
	ch, err := client.queryCh(ctx, dir.MethodFilters, nil)
	if err != nil {
		return nil, err
	}

	// collect the list of filters
	err = ch.Switch(
		channel.CollectStrings[*astral.String8](&filters),
		channel.BreakOnEOS,
		channel.PassErrors,
	)
	if err != nil {
		return nil, err
	}

	return
}
