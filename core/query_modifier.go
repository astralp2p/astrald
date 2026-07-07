package core

import (
	"slices"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

// QueryModifier is handed to preprocessors to inspect and mutate an in-flight query before routing.
type QueryModifier struct {
	query   *astral.InFlightQuery
	blocked sig.Value[error]
}

func (q *QueryModifier) Query() *astral.InFlightQuery {
	return q.query
}

// Block marks the query as rejected; routing returns RouteNotFound. Only the first block takes effect.
func (q *QueryModifier) Block(err error) {
	q.blocked.Swap(nil, err)
}

func (q *QueryModifier) Attach(object astral.Object) {
	q.query.Extra.Set(nodes.ExtraCallerProof, object)
}

// AddRelay appends a relay identity to the query, skipping duplicates.
func (q *QueryModifier) AddRelay(identity *astral.Identity) {
	if l, ok := q.query.Extra.Get(nodes.ExtraRelayVia); ok {
		if l, ok := l.([]*astral.Identity); ok {
			if slices.ContainsFunc(l, identity.IsEqual) {
				return // avoid duplicates
			}

			q.query.Extra.Replace(nodes.ExtraRelayVia, append(l, identity))
			return
		}
	}

	q.query.Extra.Set(nodes.ExtraRelayVia, []*astral.Identity{identity})
}

func (q *QueryModifier) SetCaller(identity *astral.Identity) {
	q.query.Caller = identity
}

func (q *QueryModifier) SetTarget(identity *astral.Identity) {
	q.query.Target = identity
}
