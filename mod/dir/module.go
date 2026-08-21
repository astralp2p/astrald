package dir

import (
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "dir"
const DBPrefix = "dir__"

// Module is the directory service: it maps identities to human-readable aliases,
// resolves names to identities, and applies named identity filters.
type Module interface {
	SetAlias(*astral.Identity, string) error
	GetAlias(*astral.Identity) (string, error)
	ResolveIdentity(string) (*astral.Identity, error)
	// DisplayName returns the best available human-readable name for the identity,
	// falling back to a default representation when no alias is set.
	DisplayName(*astral.Identity) string
	// AddResolver registers an additional resolver consulted during identity lookups.
	AddResolver(Resolver) error

	// SetFilter sets a function for a named filter
	SetFilter(name string, filter astral.IdentityFilter)

	// GetFilter returns the filter function for a named filter
	GetFilter(name string) astral.IdentityFilter

	// Filters method returns the list of registered filters
	Filters() []string

	// DefaultFilters returns the default list of filters applied to queries with no custom filters
	DefaultFilters() []string

	// SetDefaultFilters sets the default list of filters applied to queries with no custom filters
	SetDefaultFilters(filters ...string)

	// ApplyFilters returns true if any of the filters returns true for the provided identity
	ApplyFilters(identity *astral.Identity, filter ...string) bool
}

// Resolver is implemented by any source that can map names to identities or supply display names.
type Resolver interface {
	ResolveIdentity(string) (*astral.Identity, error)
	DisplayName(*astral.Identity) string
}
