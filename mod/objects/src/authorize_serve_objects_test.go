package objects

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/astrald"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// errRefusedByTestRouter is what countingRouter answers every query with.
var errRefusedByTestRouter = errors.New("refused by test router")

// countingRouter stands in for the node's router. It counts the queries that
// reach it and refuses each one, so a provider that queries its peer gets an
// error back instead of a stream.
type countingRouter struct {
	id *astral.Identity

	mu sync.Mutex
	n  int
}

func (r *countingRouter) RouteQuery(*astral.Context, *astral.InFlightQuery) (astral.Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return nil, errRefusedByTestRouter
}

func (r *countingRouter) GuestID() *astral.Identity { return r.id }

func (r *countingRouter) HostID() *astral.Identity { return r.id }

func (r *countingRouter) queries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// externalProvider is one row of the external provider surface in mod/objects:
// the role it serves, how it is registered, and how many of its kind the module
// holds. add returns the call the node makes on the provider.
type externalProvider struct {
	role  astral.String8
	add   func(m *Module, id *astral.Identity, c *objectscli.Client) (call func(*astral.Context) error, err error)
	count func(m *Module) int
}

func externalProviders() []externalProvider {
	objectID := &astral.ObjectID{}

	return []externalProvider{
		{
			role: auth.RoleDescriber,
			add: func(m *Module, id *astral.Identity, c *objectscli.Client) (func(*astral.Context) error, error) {
				d := &ExternalDescriber{mod: m, id: id, client: c, log: log.New(id), timeout: time.Second}
				call := func(ctx *astral.Context) error {
					_, err := d.DescribeObject(ctx, objectID)
					return err
				}
				return call, m.AddDescriber(d)
			},
			count: func(m *Module) int { return m.describers.Count() },
		},
		{
			role: auth.RoleFinder,
			add: func(m *Module, id *astral.Identity, c *objectscli.Client) (func(*astral.Context) error, error) {
				f := &ExternalFinder{mod: m, id: id, client: c, log: log.New(id), timeout: time.Second}
				call := func(ctx *astral.Context) error {
					_, err := f.FindObject(ctx, objectID)
					return err
				}
				return call, m.AddFinder(f)
			},
			count: func(m *Module) int { return m.finders.Count() },
		},
		{
			role: auth.RoleSearcher,
			add: func(m *Module, id *astral.Identity, c *objectscli.Client) (func(*astral.Context) error, error) {
				s := &ExternalSearcher{mod: m, id: id, client: c, log: log.New(id), timeout: time.Second}
				call := func(ctx *astral.Context) error {
					_, err := s.SearchObject(ctx, objects.SearchQuery{})
					return err
				}
				return call, m.AddSearcher(s)
			},
			count: func(m *Module) int { return m.searchers.Count() },
		},
	}
}

// TestExternalProviderSkippedWhenNotAuthorized is the revocation measure for
// external providers: a provider that holds no ServeObjects grant for its role
// is not queried, and stays registered.
func TestExternalProviderSkippedWhenNotAuthorized(t *testing.T) {
	for _, p := range externalProviders() {
		t.Run(string(p.role), func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			router := &countingRouter{id: astral.GenerateIdentity()}
			id := astral.GenerateIdentity()

			call, err := p.add(mod, id, objectscli.New(id, astrald.New(router)))
			if err != nil {
				t.Fatalf("register %s: %v", p.role, err)
			}

			err = call(astral.NewContext(nil))
			if !errors.Is(err, objectsmod.ErrExternalNotAuthorized) {
				t.Fatalf("unauthorized %s answered: got err %v, want %v", p.role, err, objectsmod.ErrExternalNotAuthorized)
			}

			if n := router.queries(); n != 0 {
				t.Fatalf("unauthorized %s was queried %d times; want 0", p.role, n)
			}

			if n := p.count(mod); n != 1 {
				t.Fatalf("unauthorized %s: module holds %d, want it still registered", p.role, n)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("Authorize called %d times; want 1", len(actions))
			}

			action, ok := actions[0].(*auth.ServeObjectsAction)
			if !ok {
				t.Fatalf("authorized %T; want *auth.ServeObjectsAction", actions[0])
			}
			if !action.Actor().IsEqual(id) {
				t.Fatalf("authorized actor %v; want the provider %v", action.Actor(), id)
			}
			if action.Role != p.role {
				t.Fatalf("authorized role %q; want %q", action.Role, p.role)
			}
		})
	}
}

// TestExternalProviderResumesWhenAuthorized guards the inverse: once the
// provider is authorized again, the next call queries it without a new
// registration.
func TestExternalProviderResumesWhenAuthorized(t *testing.T) {
	for _, p := range externalProviders() {
		t.Run(string(p.role), func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			router := &countingRouter{id: astral.GenerateIdentity()}
			id := astral.GenerateIdentity()

			call, err := p.add(mod, id, objectscli.New(id, astrald.New(router)))
			if err != nil {
				t.Fatalf("register %s: %v", p.role, err)
			}

			_ = call(astral.NewContext(nil))

			authority.mu.Lock()
			authority.verdict = true
			authority.mu.Unlock()

			err = call(astral.NewContext(nil))
			if !errors.Is(err, errRefusedByTestRouter) {
				t.Fatalf("authorized %s: got err %v, want the router's refusal", p.role, err)
			}

			if n := router.queries(); n != 1 {
				t.Fatalf("authorized %s was queried %d times; want 1", p.role, n)
			}
		})
	}
}
