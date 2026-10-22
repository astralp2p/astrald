package apphost

import (
	"io"
	"net"
	"testing"

	usermod "github.com/astralp2p/astrald/mod/user"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// stubUser satisfies usermod.Module; only Ready and Identity are exercised.
type stubUser struct {
	id    *astral.Identity
	ready chan struct{}
}

func (s *stubUser) Ready() <-chan struct{}                                        { return s.ready }
func (s *stubUser) Identity() *astral.Identity                                    { return s.id }
func (s *stubUser) LocalSwarm() []*astral.Identity                                { return nil }
func (s *stubUser) NewMaintainLinkTask(*astral.Identity) usermod.MaintainLinkTask { return nil }
func (s *stubUser) NewSyncNodesTask(*astral.Identity) usermod.SyncNodesTask       { return nil }
func (s *stubUser) PushToLocalSwarm(*astral.Context, astral.Object)               {}
func (s *stubUser) PushToSiblings(*astral.Context, astral.Object)                 {}
func (s *stubUser) Expel(*astral.Context, *astral.Identity) (*user.SignedExpulsion, error) {
	return nil, nil
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func TestInSetupMode(t *testing.T) {
	ctx := astral.NewContext(nil)

	// no user module at all
	if got := (&Module{}).inSetupMode(ctx); !got {
		t.Fatal("nil User: want setup mode")
	}

	// user module ready, no active user
	noUser := &Module{}
	noUser.User = &stubUser{id: nil, ready: closedChan()}
	if got := noUser.inSetupMode(ctx); !got {
		t.Fatal("ready + no identity: want setup mode")
	}

	// user module ready, active user present
	hasUser := &Module{}
	hasUser.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}
	if got := hasUser.inSetupMode(ctx); got {
		t.Fatal("ready + identity: want NOT setup mode")
	}

	// user module not ready yet, ctx cancelled -> err toward setup mode
	notReady := &Module{}
	notReady.User = &stubUser{id: astral.GenerateIdentity(), ready: make(chan struct{})}
	cctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()
	if got := notReady.inSetupMode(cctx); !got {
		t.Fatal("not ready + ctx cancelled: want setup mode")
	}
}

const trustedOrigin = "https://settings.example"

func withAllowlist(m *Module) *Module {
	m.config.AllowAnonymous = true
	m.config.TrustedWebSources = map[string][]PermitConfig{trustedOrigin: nil}
	m.config.AnonymousAllowlist = AnonymousAllowlist{
		Unclaimed: []string{"user.info", "tree.set"},
		Claimed:   []string{"apphost.register"},
	}
	return m
}

func TestAdmitsAnonymous(t *testing.T) {
	ctx := astral.NewContext(nil)

	unclaimed := withAllowlist(&Module{})
	unclaimed.User = &stubUser{id: nil, ready: closedChan()}

	claimed := withAllowlist(&Module{})
	claimed.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}

	closed := withAllowlist(&Module{})
	closed.User = &stubUser{id: nil, ready: closedChan()}
	closed.config.AllowAnonymous = false

	q := func(s string) *astral.Query { return &astral.Query{QueryString: astral.String32(s)} }

	cases := []struct {
		name   string
		mod    *Module
		origin string
		query  string
		want   bool
	}{
		// unclaimed: confined to the unclaimed list
		{"unclaimed, listed op", unclaimed, "", "user.info", true},
		{"unclaimed, listed op with params", unclaimed, "", "user.info?in=json", true},
		{"unclaimed, non-listed op", unclaimed, "", "apphost.list_tokens", false},

		// unclaimed web: only a trusted origin reaches the unclaimed list
		{"unclaimed, trusted origin, listed op", unclaimed, trustedOrigin, "user.info", true},
		{"unclaimed, untrusted origin, listed op", unclaimed, "https://evil.example", "user.info", false},

		// claimed: confined to the claimed list - former setup ops now refused
		{"claimed, listed op", claimed, "", "apphost.register", true},
		{"claimed, unclaimed-only op refused", claimed, "", "user.info", false},
		{"claimed, non-listed op", claimed, "", "user.expel", false},
		{"claimed, untrusted origin, listed op", claimed, "https://evil.example", "apphost.register", true},

		// allow_anonymous false refuses even a listed op
		{"anonymous not allowed, listed op", closed, "", "user.info", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.mod.admitsAnonymous(ctx, c.origin, q(c.query)); got != c.want {
				t.Fatalf("admitsAnonymous = %v; want %v", got, c.want)
			}
		})
	}
}

// callerNode records the caller of each query routed through it and routes
// none.
type callerNode struct {
	astral.Node
	id     *astral.Identity
	caller chan *astral.Identity
}

func (n *callerNode) Identity() *astral.Identity { return n.id }

func (n *callerNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, _ io.WriteCloser) (io.WriteCloser, error) {
	n.caller <- q.Caller
	return query.RouteNotFound()
}

// TestRouteQueryNamesTheSessionForAnEmptyCaller: core.Router names an empty
// caller the node, so a session with a token that leaves the caller empty
// reaches the router as its own identity.
func TestRouteQueryNamesTheSessionForAnEmptyCaller(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	go io.Copy(io.Discard, cli)

	node := &callerNode{id: astral.GenerateIdentity(), caller: make(chan *astral.Identity, 1)}
	guest := NewGuest(&Module{node: node, log: log.New(nil)}, srv)
	guest.guestID = astral.GenerateIdentity()

	err := guest.onRouteQueryMsg(astral.NewContext(nil), &apphost.RouteQueryMsg{
		Nonce: astral.NewNonce(),
		Query: "user.expel",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if got := <-node.caller; !got.IsEqual(guest.guestID) {
		t.Fatalf("caller = %v; want the session's identity %v", got, guest.guestID)
	}
}
