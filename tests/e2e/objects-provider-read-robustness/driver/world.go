package main

import (
	"fmt"
	"os"

	"github.com/astralp2p/astral-go/api/auth"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// world is the flow's cast on node1.
type world struct {
	ctx   *astral.Context
	user  *astrald.Client
	node1 *astral.Identity
	node2 *astral.Identity
	o1    *astral.ObjectID
	o2    *astral.ObjectID

	f, h, l, g *provider.Provider // finder, hanging finder, liar, silent reader
	d          *provider.App      // registered, never served
	r          *astral.Identity   // known to no node

	stops []func()
}

// close stops every app and revokes ServeObjects from every app holding it,
// so nothing stays registered in the world.
func (w *world) close() {
	for _, p := range []*provider.Provider{w.f, w.h, w.l} {
		if p == nil {
			continue
		}
		if err := provider.Revoke(w.ctx, w.user, p.App.ID, provider.ServeObjects); err != nil {
			fmt.Fprintf(os.Stderr, "revoke ServeObjects of %v: %v\n", p.App.ID, err)
		}
	}
	for _, stop := range w.stops {
		stop()
	}
}

func setUp(path, o1 string, f *facts) (*world, error) {
	s, err := provider.Load(path)
	if err != nil {
		return nil, err
	}
	n1, err := s.Node("node1")
	if err != nil {
		return nil, err
	}
	n2, err := s.Node("node2")
	if err != nil {
		return nil, err
	}
	token, err := s.UserToken()
	if err != nil {
		return nil, err
	}
	w := &world{ctx: astral.NewContext(nil), user: provider.Connect(n1.Endpoint, token), r: astral.GenerateIdentity()}
	if w.node1, err = n1.ID(); err != nil {
		return w, err
	}
	if w.node2, err = n2.ID(); err != nil {
		return w, err
	}
	if w.o1, err = astral.ParseID(o1); err != nil {
		return w, fmt.Errorf("o1 %q: %w", o1, err)
	}
	nonce := astral.NewNonce().String()
	if w.o2, err = astral.ResolveObjectID(astral.NewString16("objects-provider-read-robustness O2 " + nonce)); err != nil {
		return w, err
	}
	if err := w.startApps(n1.Endpoint); err != nil {
		return w, err
	}
	f.O1, f.O2 = w.o1.String(), w.o2.String()
	f.Names = map[string]string{
		w.node1.String(): "node1", w.node2.String(): "node2", w.r.String(): "R", w.d.ID.String(): "D",
		w.f.App.ID.String(): "F", w.h.App.ID.String(): "H", w.l.App.ID.String(): "L", w.g.App.ID.String(): "G",
	}
	return w, nil
}

// startApps registers the apps as the User and serves F, H, L and G; D is
// registered and never served.
func (w *world) startApps(endpoint string) error {
	all, err := provider.ServeObjectsPermit()
	if err != nil {
		return err
	}
	liar, err := provider.ServeObjectsPermit(auth.RoleDescriber, auth.RoleSearcher)
	if err != nil {
		return err
	}
	if w.f, err = w.serve(endpoint, all, provider.Find); err != nil {
		return fmt.Errorf("finder F: %w", err)
	}
	w.f.SetMode(provider.Find, provider.Claim)
	if w.h, err = w.serve(endpoint, all, provider.Find); err != nil {
		return fmt.Errorf("finder H: %w", err)
	}
	w.h.SetMode(provider.Find, provider.Hang)
	if w.l, err = w.serve(endpoint, liar, provider.Describe, provider.Search); err != nil {
		return fmt.Errorf("liar L: %w", err)
	}
	if w.g, err = w.serve(endpoint, nil); err != nil {
		return fmt.Errorf("silent reader G: %w", err)
	}
	w.g.SetMode(provider.Read, provider.Hang)
	if w.d, err = provider.Register(w.ctx, w.user, endpoint); err != nil {
		return fmt.Errorf("dead app D: %w", err)
	}
	return nil
}

// roleOf is the registration each op a provider serves for node1 needs.
var roleOf = map[provider.Op]astral.String8{
	provider.Find: auth.RoleFinder, provider.Describe: auth.RoleDescriber, provider.Search: auth.RoleSearcher,
}

// serve registers an app with permit (none when nil) and serves it; each of
// ops is registered with node1 from a registration hook, with lease.
func (w *world) serve(endpoint string, permit *auth.Permit, ops ...provider.Op) (*provider.Provider, error) {
	var permits []*auth.Permit
	if permit != nil {
		permits = append(permits, permit)
	}
	app, err := provider.Register(w.ctx, w.user, endpoint, permits...)
	if err != nil {
		return nil, err
	}
	p := provider.New(app)
	var hooks []apps.RegistrationHook
	for _, op := range ops {
		role := roleOf[op]
		hooks = append(hooks, func(ctx *astral.Context) error {
			_, err := provider.RegisterRole(ctx, objectscli.New(nil, app.Client), role, lease)
			return err
		})
	}
	stop, err := p.Serve(w.ctx, hooks...)
	if err != nil {
		return nil, err
	}
	w.stops = append(w.stops, stop)
	return p, nil
}
