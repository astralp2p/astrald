package dir

import (
	"errors"
	"fmt"
	"os"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/aliasgen"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/resources"
	"gorm.io/gorm"
)

const ZeroIdentity = "<anyone>"

type Deps struct {
	Nearby nearby.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	db     *gorm.DB

	ops *routing.OpRouter

	resolvers      sig.Set[dir.Resolver]
	filters        sig.Map[string, astral.IdentityFilter]
	defaultFilters []string
}

var _ dir.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) AddResolver(resolver dir.Resolver) error {
	return mod.resolvers.Add(resolver)
}

// ResolveIdentity resolves a name to an identity in priority order: special keywords ("", "anyone", "localnode"),
// raw identity string, local alias DB, then registered external resolvers.
func (mod *Module) ResolveIdentity(s string) (identity *astral.Identity, err error) {
	if s == "" || s == "anyone" {
		return &astral.Identity{}, nil
	}

	if s == "localnode" {
		return mod.node.Identity(), nil
	}

	if identity, err := astral.ParseIdentity(s); err == nil {
		return identity, nil
	}

	err = mod.db.
		Model(&dbAlias{}).
		Where("alias = ?", s).
		Select("identity").
		First(&identity).
		Error
	if err == nil {
		return
	}

	for _, r := range mod.resolvers.Clone() {
		if i, err := r.ResolveIdentity(s); err == nil {
			return i, nil
		}
	}

	return nil, fmt.Errorf("unknown identity: %s", s)
}

// DisplayName returns a human-readable name for an identity, falling back through alias, registered resolvers,
// and finally the identity fingerprint. Returns ZeroIdentity for the zero value.
func (mod *Module) DisplayName(identity *astral.Identity) string {
	if identity.IsZero() {
		return ZeroIdentity
	}

	a, err := mod.GetAlias(identity)
	if err == nil {
		return a
	}

	for _, r := range mod.resolvers.Clone() {
		if s := r.DisplayName(identity); len(s) > 0 {
			return s
		}
	}

	return identity.Fingerprint()
}

func (mod *Module) SetFilter(name string, filter astral.IdentityFilter) {
	mod.filters.Replace(name, filter)
}

func (mod *Module) GetFilter(name string) (filter astral.IdentityFilter) {
	filter, _ = mod.filters.Get(name)
	return
}

func (mod *Module) Filters() []string {
	return mod.filters.Keys()
}

func (mod *Module) DefaultFilters() []string {
	return mod.defaultFilters
}

func (mod *Module) SetDefaultFilters(filters ...string) {
	mod.defaultFilters = filters
}

// ApplyFilters returns true if the identity passes any of the named filters (OR semantics).
// Unknown filter names are silently skipped.
func (mod *Module) ApplyFilters(identity *astral.Identity, filter ...string) bool {
	for _, f := range filter {
		filter := mod.GetFilter(f)
		if filter == nil {
			continue
		}
		if filter(identity) {
			return true
		}
	}
	return false
}

func (mod *Module) Router() astral.Router {
	return mod.ops
}

func (mod *Module) String() string {
	return dir.ModuleName
}

// setDefaultAlias sets a default node alias if no alias is set. It uses hostname if set, or a random name.
func (mod *Module) setDefaultAlias() error {
	// check the current alias
	alias, err := mod.GetAlias(mod.node.Identity())
	switch {
	case (err != nil) && (!errors.Is(err, gorm.ErrRecordNotFound)):
		return err // error other than not found
	case alias != "":
		return nil // there's an alias already
	}

	// generate a random alias
	alias = aliasgen.New()

	// check for custom name for the local device
	hostname, err := os.Hostname()
	if err == nil {
		if hostname != "" && hostname != "localhost" {
			//alias = hostname
		}
	}

	err = mod.SetAlias(mod.node.Identity(), alias)
	if err != nil {
		return err
	}

	mod.log.Info("call me %v", styles.String(alias, theme.Identity))

	return nil
}
