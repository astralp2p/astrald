package main

import (
	dircli "github.com/cryptopunkscc/astral-go/api/dir/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/log/views"
)

func loadAliasMap(ctx *astral.Context) (err error) {
	aliasMap, err := dircli.Default().AliasMap(ctx)
	if err != nil {
		return
	}

	views.IdentityResolver.Set(newResolver(aliasMap))

	return
}
