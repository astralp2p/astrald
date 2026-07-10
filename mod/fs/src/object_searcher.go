package fs

import (
	"strings"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

// SearchObject performs a case-insensitive path substring search; requires the "path" tag and ZoneDevice zone.
func (mod *Module) SearchObject(ctx *astral.Context, query objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	err := query.RequiredTagsIn("path")
	if err != nil {
		return nil, err
	}

	var results = make(chan *objects.SearchResult)

	go func() {
		defer close(results)

		rows, err := mod.db.SearchByPath(strings.ToLower(string(query.Query)))
		if err != nil {
			mod.log.Error("search: db: %v", err)
			return
		}
		mod.log.Log("search fs: %v", len(rows))
		for _, row := range rows {

			results <- &objects.SearchResult{
				SourceID: mod.node.Identity(),
				ObjectID: row.DataID,
			}
		}
	}()

	return results, nil
}
