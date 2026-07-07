package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

// Searcher is used to search for objects matching a query
type Searcher interface {
	SearchObject(ctx *astral.Context, query SearchQuery) (<-chan *SearchResult, error)
}

// SearchPreprocessor is a hook that mutates a Search in place before it runs.
type SearchPreprocessor interface {
	PreprocessSearch(*Search)
}

type Search struct {
	CallerID *astral.Identity
	Query    SearchQuery
	Sources  []*astral.Identity
}
