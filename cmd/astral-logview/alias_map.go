package main

import (
	dircli "github.com/astralp2p/astral-go/api/dir/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/log/views"
)

func loadAliasMap(ctx *astral.Context) (err error) {
	aliasMap, err := dircli.Default().AliasMap(ctx)
	if err != nil {
		return
	}

	views.IdentityResolver.Set(newResolver(aliasMap))

	return
}
