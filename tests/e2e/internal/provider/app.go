package provider

import (
	"fmt"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
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
// register apps there (the User or the node), and grants it permits. The node
// adds ServeApps and UseBlueprints to every registration.
func Register(ctx *astral.Context, registrar *astrald.Client, n Node, permits ...*auth.Permit) (*App, error) {
	tok, err := apphostcli.New(nil, registrar).Register(ctx, permits...)
	if err != nil {
		return nil, fmt.Errorf("apphost.register: %w", err)
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

// Grant gives id permits on the node of admin, a client that may manage apps
// there.
func Grant(ctx *astral.Context, admin *astrald.Client, id *astral.Identity, permits ...*auth.Permit) error {
	return apphostcli.New(nil, admin).Grant(ctx, &apphostapi.GrantRequest{Identity: id, Permits: permits})
}

// Revoke removes every grant of action from id on the node of admin.
func Revoke(ctx *astral.Context, admin *astrald.Client, id *astral.Identity, action string) error {
	return apphostcli.New(nil, admin).RevokeAction(ctx, id, action)
}
