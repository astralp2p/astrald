package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

// PreprocessQuery blocks outgoing queries whose target fails the active directory filters.
// Queries to the local node are always allowed; filters may be overridden per-query via the "filters" extra key.
func (mod *Module) PreprocessQuery(m *core.QueryModifier) error {
	q := m.Query()

	// never block queries to the local node
	if q.Target.IsEqual(mod.node.Identity()) {
		return nil
	}

	var filters = mod.DefaultFilters()

	// copy filters from the query if provided
	if value, ok := q.Extra.Get("filters"); ok {
		if list, ok := value.([]string); ok && len(list) > 0 {
			filters = list
		}
	}

	// do nothing if there are no filters to apply
	if len(filters) == 0 {
		return nil
	}

	// apply the filters
	if !mod.ApplyFilters(q.Target, filters...) {
		return astral.ErrTargetNotAllowed
	}

	return nil
}
