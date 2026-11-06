package sys

import (
	"sync"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/sys"
	"github.com/astralp2p/astrald/mod/tree"
	usermod "github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Apphost apphostmod.Module
	Auth    authmod.Module
	Tree    tree.Module
}

type OptionalDeps struct {
	User usermod.Module
}

// appProvisioner mints an App identity: a key pair, a signed app contract
// and an access token.
type appProvisioner interface {
	ProvisionApp(ctx *astral.Context, grantPermits, contractPermits []*auth.Permit) (*apphost.AccessToken, error)
}

// policyStore stores or clears the delegate of one named policy.
type policyStore interface {
	SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error
}

type Module struct {
	Deps
	OptionalDeps

	node   astral.Node
	log    *log.Logger
	router routing.OpRouter

	// apps provisions the manager's App identity; Deps.Apphost in a node.
	apps appProvisioner

	// apphostPolicies stores the apphost policy delegates; Deps.Apphost in a
	// node.
	apphostPolicies policyStore

	// appointment persists the manager sys.register_manager appointed.
	appointment appointmentConfig

	// legacy reads the manager record of a node that apphost appointed.
	legacy appointmentConfig

	// mu makes the fresh check and the appointment one transition; see
	// OpRegisterManager.
	mu sync.Mutex
}

var _ sys.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return sys.ModuleName
}
