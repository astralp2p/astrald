// Command driver checks who node1 lets register as an object provider. Eight
// apps, alike but for their permits, call objects.register_finder,
// register_describer and register_searcher with their own tokens; node1's own
// token and node2's token, over the link, call them too. The User's find,
// describe and search on node1 then dial exactly the apps whose registration
// was accepted.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same matrix with fresh apps
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// lease is the registration every cell asks for. It lapses 10 s after the
// driver, so no later test in the world meets these apps in node1's fan-out.
const lease = astral.Duration(10 * time.Second)

// callTimeout bounds one register call and one fan-out.
const callTimeout = 30 * time.Second

// leaseMargin is how long after the first accepted lease the fan-out must
// have started, so every accepted registration is still live when it runs.
const leaseMargin = 8 * time.Second

const (
	notPermitted  = "not permitted"
	selfRefusal   = "node identity cannot register as an external discoverer"
	originRefusal = "origin not allowed: network"
)

// cellApp is one app of the matrix: its permits and the roles node1 must
// accept it for.
type cellApp struct {
	name     string
	permits  func() ([]*auth.Permit, error)
	accepted []astral.String8
}

func serveObjects(roles ...astral.String8) func() ([]*auth.Permit, error) {
	return func() ([]*auth.Permit, error) {
		p, err := provider.ServeObjectsPermit(roles...)
		return []*auth.Permit{p}, err
	}
}

var matrix = []cellApp{
	{"all", serveObjects(), provider.Roles},
	{"d", serveObjects(auth.RoleDescriber), []astral.String8{auth.RoleDescriber}},
	{"f", serveObjects(auth.RoleFinder), []astral.String8{auth.RoleFinder}},
	{"s", serveObjects(auth.RoleSearcher), []astral.String8{auth.RoleSearcher}},
	{"df", serveObjects(auth.RoleDescriber, auth.RoleFinder), []astral.String8{auth.RoleDescriber, auth.RoleFinder}},
	{"i", serveObjects(auth.RoleIndexer), nil},
	{"store", func() ([]*auth.Permit, error) { return []*auth.Permit{provider.StoreObjectsPermit()}, nil }, nil},
	{"none", func() ([]*auth.Permit, error) { return nil, nil }, nil},
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	f := newFacts(os.Args[1])
	err := run(os.Args[2], f)
	if os.Args[1] == "drive" {
		provider.Emit("objects_provider_registration_authz", f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// facts is what the driver records: every reply and dial count by
// "<app>/<role>", and which checks held.
type facts struct {
	Cells       map[string]string `json:"cells"`
	NodeSelf    map[string]string `json:"node_self"`
	NodeNetwork map[string]string `json:"node_network"`
	Dials       map[string]int    `json:"dials"`
	FanoutAfter float64           `json:"fanout_after_s"`
	Checks      map[string]bool   `json:"checks"`

	checks *provider.Checks
}

// newFacts returns empty facts whose checks end the flow in mode "verify"
// only; in mode "drive" the oracle judges them.
func newFacts(mode string) *facts {
	c := provider.NewChecks(mode)
	return &facts{Cells: map[string]string{}, NodeSelf: map[string]string{},
		NodeNetwork: map[string]string{}, Dials: map[string]int{}, Checks: c.Held, checks: c}
}

// reply renders one register answer: "lease <duration>" or "error: <text>".
func reply(l *objects.RegistrationLease, err error) string {
	switch {
	case err != nil:
		return "error: " + err.Error()
	case l == nil:
		return "no answer"
	}
	return "lease " + time.Duration(l.Duration).String()
}

func run(path string, f *facts) error {
	s, err := provider.Load(path)
	if err != nil {
		return err
	}
	n1, err := s.Node("node1")
	if err != nil {
		return err
	}
	n2, err := s.Node("node2")
	if err != nil {
		return err
	}
	token, err := s.UserToken()
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	user := provider.Connect(n1.Endpoint, token)

	providers, stop, err := startApps(ctx, user, n1)
	defer stop()
	if err != nil {
		return err
	}
	defer revokeAll(ctx, user, providers)

	firstLease, err := registerMatrix(ctx, providers, f)
	if err != nil {
		return err
	}
	if err := registerNodes(ctx, n1, n2, f); err != nil {
		return err
	}
	current, err := fanOut(ctx, user, firstLease, f)
	if err != nil {
		return err
	}
	return checkDials(providers, current, f)
}

// startApps registers every app of the matrix as the User and serves it with
// counting handlers. stop ends every app started, also on an error.
func startApps(ctx *astral.Context, user *astrald.Client, n1 provider.Node) (map[string]*provider.Provider, func(), error) {
	providers := map[string]*provider.Provider{}
	var stops []func()
	stop := func() {
		for _, s := range stops {
			s()
		}
	}
	for _, c := range matrix {
		permits, err := c.permits()
		if err != nil {
			return providers, stop, err
		}
		app, err := provider.Register(ctx, user, n1, permits...)
		if err != nil {
			return providers, stop, fmt.Errorf("app %s: %w", c.name, err)
		}
		p := provider.New(app)
		s, err := p.Serve(ctx)
		if err != nil {
			return providers, stop, fmt.Errorf("app %s: %w", c.name, err)
		}
		stops = append(stops, s)
		providers[c.name] = p
	}
	return providers, stop, nil
}

func accepts(c cellApp, role astral.String8) bool {
	for _, r := range c.accepted {
		if r == role {
			return true
		}
	}
	return false
}

// registerMatrix makes every app call every register op with its own token,
// records each reply, and checks the accepted/refused split. It returns when
// the first accepted lease was granted.
func registerMatrix(ctx *astral.Context, providers map[string]*provider.Provider, f *facts) (time.Time, error) {
	var first time.Time
	var bad []string
	for _, c := range matrix {
		for _, role := range provider.Roles {
			cctx, cancel := ctx.WithTimeout(callTimeout)
			l, err := providers[c.name].App.RegisterAs(cctx, role, lease)
			cancel()
			cell := c.name + "/" + string(role)
			f.Cells[cell] = reply(l, err)
			if err == nil && first.IsZero() {
				first = time.Now()
			}
			if !cellHeld(accepts(c, role), l, err) {
				bad = append(bad, cell+": "+f.Cells[cell])
			}
		}
	}
	return first, f.checks.Check("matrix", len(bad) == 0,
		"cells that answered otherwise than accepted with a 10 s lease or refused with %q: %v", notPermitted, bad)
}

// cellHeld reports whether one register reply is what the matrix wants: a
// 10 s lease for an accepted cell, exactly "not permitted" for a refused one.
func cellHeld(accepted bool, l *objects.RegistrationLease, err error) bool {
	if accepted {
		return err == nil && l != nil && l.Duration == lease
	}
	return err != nil && err.Error() == notPermitted
}

// registerNodes calls the three register ops with node1's own token, and with
// node2's token over the link to node1.
func registerNodes(ctx *astral.Context, n1, n2 provider.Node, f *facts) error {
	node1, err := n1.ID()
	if err != nil {
		return err
	}
	self := objectscli.New(nil, n1.Admin())
	network := objectscli.New(node1, n2.Admin())
	var selfBad, netBad []string
	for _, role := range provider.Roles {
		cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(callTimeout)
		l, err := provider.RegisterRole(cctx, self, role, lease)
		f.NodeSelf[string(role)] = reply(l, err)
		if err == nil || (err.Error() != notPermitted && err.Error() != selfRefusal) {
			selfBad = append(selfBad, string(role)+": "+f.NodeSelf[string(role)])
		}
		l, err = provider.RegisterRole(cctx, network, role, lease)
		cancel()
		f.NodeNetwork[string(role)] = reply(l, err)
		if err == nil || err.Error() != originRefusal {
			netBad = append(netBad, string(role)+": "+f.NodeNetwork[string(role)])
		}
	}
	if err := f.checks.Check("node_self_refused", len(selfBad) == 0,
		"node1's own token was not refused with %q or %q: %v", notPermitted, selfRefusal, selfBad); err != nil {
		return err
	}
	return f.checks.Check("network_refused", len(netBad) == 0,
		"node2's token over the link was not refused with %q: %v", originRefusal, netBad)
}
