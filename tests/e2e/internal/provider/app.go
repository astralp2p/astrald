package provider

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"
)

// ServeObjects is the action every object-provider registration answers to.
const ServeObjects = "mod.auth.serve_objects_action"

// The three roles a provider registers for.
var Roles = []astral.String8{auth.RoleFinder, auth.RoleDescriber, auth.RoleSearcher}

// ServeObjectsPermit is a ServeObjects permit limited to roles; no roles is
// the unconstrained permit, which covers every role.
func ServeObjectsPermit(roles ...astral.String8) (*auth.Permit, error) {
	p := &auth.Permit{Action: ServeObjects}
	if len(roles) == 0 {
		return p, nil
	}
	cs := astral.NewBundle()
	for _, r := range roles {
		if err := cs.Append(astral.NewString8(string(r))); err != nil {
			return nil, err
		}
	}
	p.Constraints = cs
	return p, nil
}

// StoreObjectsPermit is an unconstrained StoreObjects permit.
func StoreObjectsPermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.StoreObjectsAction{}.ObjectType())}
}

// App is a registered app identity with a client of its own on the node at
// Endpoint. The node dials the app's handler at HostIP.
type App struct {
	ID       *astral.Identity
	Token    string
	Endpoint string
	HostIP   string
	Client   *astrald.Client
}

// Register mints a fresh app on node n through registrar, a client that may
// register apps there (the User or the node), and waits until its host signed
// the contract its registration requested (contracts.Await). The node adds
// ServeApps and UseBlueprints to every registration.
func Register(ctx *astral.Context, registrar *astrald.Client, n Node, permits ...*auth.Permit) (*App, error) {
	tok, err := contracts.Register(ctx, registrar, n.Endpoint, permits...)
	if err != nil {
		return nil, err
	}
	return &App{
		ID:       tok.Identity,
		Token:    string(tok.Token),
		Endpoint: n.Endpoint,
		HostIP:   n.HostIP,
		Client:   Connect(n.Endpoint, string(tok.Token)),
	}, nil
}

// RegisterAs calls objects.register_finder, register_describer or
// register_searcher, by role, with the app's own token on its node.
func (a *App) RegisterAs(ctx *astral.Context, role astral.String8, lease astral.Duration) (*objects.RegistrationLease, error) {
	return RegisterRole(ctx, objectscli.New(nil, a.Client), role, lease)
}

// RegisterRole calls the register op of role through cli.
func RegisterRole(ctx *astral.Context, cli *objectscli.Client, role astral.String8, lease astral.Duration) (*objects.RegistrationLease, error) {
	switch role {
	case auth.RoleFinder:
		return cli.RegisterFinder(ctx, lease)
	case auth.RoleDescriber:
		return cli.RegisterDescriber(ctx, lease)
	case auth.RoleSearcher:
		return cli.RegisterSearcher(ctx, lease)
	}
	return nil, fmt.Errorf("no register op for role %q", role)
}

// Grant has the app a's host sign it a contract for permits: a fresh
// ContractRequest, approved by the harness answerer (contracts.Request).
func Grant(ctx *astral.Context, a *App, permits ...*auth.Permit) error {
	_, err := contracts.Request(ctx, a.Client, permits...)
	return err
}
