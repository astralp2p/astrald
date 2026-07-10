package main

import (
	"errors"
	dirmod "github.com/astralp2p/astrald/mod/dir"

	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/astral"
)

// resolver implements dir.Resolver from cache
type resolver struct {
	aliasMap *dir.AliasMap
	revMap   map[string]string
}

var _ dirmod.Resolver = &resolver{}

func newResolver(aliasMap *dir.AliasMap) *resolver {
	r := &resolver{
		aliasMap: aliasMap,
		revMap:   make(map[string]string),
	}

	if aliasMap != nil {
		for k, id := range aliasMap.Aliases {
			r.revMap[id.String()] = k
		}
	}

	return r
}

func (r resolver) ResolveIdentity(s string) (*astral.Identity, error) {
	if r.aliasMap == nil {
		return nil, errors.New("resolution failed")
	}
	id, ok := r.aliasMap.Aliases[s]
	if !ok {
		return nil, errors.New("resolution failed")
	}
	return id, nil
}

func (r resolver) DisplayName(identity *astral.Identity) string {
	name := r.revMap[identity.String()]
	if name == "" {
		return identity.String()
	}
	return name
}
