package main

import (
	"github.com/cryptopunkscc/astral-go/astral"
	dircli "github.com/cryptopunkscc/astrald/mod/dir/client"
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
