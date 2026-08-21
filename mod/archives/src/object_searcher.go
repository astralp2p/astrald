package archives

import (
	"strings"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

// SearchObject performs a case-insensitive path substring search over indexed
// archive entries. Only serves ZoneVirtual; requires the "path" or "archive" tag.
func (mod *Module) SearchObject(ctx *astral.Context, query objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	if !ctx.Zone().Is(astral.ZoneVirtual) {
		return nil, astral.ErrZoneExcluded
	}

	err := query.RequiredTagsIn("path", "archive")
	if err != nil {
		return nil, err
	}

	var results = make(chan *objects.SearchResult)

	go func() {
		defer close(results)

		var rows []*dbEntry

		q := "%" + strings.ToLower(string(query.Query)) + "%"

		err := mod.db.Where("LOWER(path) LIKE ?", q).Find(&rows).Error
		if err != nil {
			mod.log.Error("search: db: %v", err)
			return
		}

		for _, row := range rows {
			results <- &objects.SearchResult{
				SourceID: mod.node.Identity(),
				ObjectID: row.ObjectID,
			}
		}
	}()

	return results, nil
}
