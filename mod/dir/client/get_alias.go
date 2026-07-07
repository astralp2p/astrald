package dir

import (
	"github.com/cryptopunkscc/astral-go/api/dir"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// GetAlias returns the human-readable alias for the given identity, consulting the
// in-memory cache before making a remote query when EnableCache is set.
func (client *Client) GetAlias(ctx *astral.Context, identity *astral.Identity) (alias string, err error) {
	// check cache
	if client.EnableCache {
		if alias, ok := client.aliasCache.Get(identity.String()); ok {
			return alias, nil
		}
	}

	// query
	ch, err := client.queryCh(ctx, dir.MethodGetAlias, query.Args{
		"id": identity,
	})
	if err != nil {
		return "", err
	}

	err = ch.Switch(channel.ExpectString[*astral.String8](&alias), channel.PassErrors)
	if err != nil {
		return "", err
	}

	// save to cache
	if client.EnableCache {
		client.aliasCache.Set(identity.String(), alias)
	}

	return
}

func GetAlias(ctx *astral.Context, identity *astral.Identity) (string, error) {
	return Default().GetAlias(ctx, identity)
}
