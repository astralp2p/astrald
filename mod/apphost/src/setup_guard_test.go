package apphost

import (
	"testing"

	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/mod/user"
)

// stubUser satisfies user.Module; only Ready and Identity are exercised.
type stubUser struct {
	id    *astral.Identity
	ready chan struct{}
}

func (s *stubUser) Ready() <-chan struct{}                                     { return s.ready }
func (s *stubUser) Identity() *astral.Identity                                 { return s.id }
func (s *stubUser) LocalSwarm() []*astral.Identity                             { return nil }
func (s *stubUser) NewMaintainLinkTask(*astral.Identity) user.MaintainLinkTask { return nil }
func (s *stubUser) NewSyncNodesTask(*astral.Identity) user.SyncNodesAction     { return nil }
func (s *stubUser) PushToLocalSwarm(*astral.Context, astral.Object)            {}
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

func withAllowlist(m *Module) *Module {
	m.config.AnonymousWebAllowlist = AnonymousWebAllowlist{
		Unclaimed: []string{"user.info", "tree.set"},
		Claimed:   []string{"apphost.register"},
	}
	return m
}

func TestBlocksAnonymousWeb(t *testing.T) {
	ctx := astral.NewContext(nil)

	unclaimed := withAllowlist(&Module{})
	unclaimed.User = &stubUser{id: nil, ready: closedChan()}

	claimed := withAllowlist(&Module{})
	claimed.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}

	const origin = "https://settings.astrald.app"
	q := func(s string) *astral.Query { return &astral.Query{QueryString: s} }

	cases := []struct {
		name          string
		mod           *Module
		webOrigin     string
		authenticated bool
		query         string
		want          bool
	}{
		// IPC (empty origin) is never gated, in either claim state
		{"IPC, unclaimed, non-listed op", unclaimed, "", false, "apphost.list_tokens", false},
		{"IPC, claimed, non-listed op", claimed, "", false, "user.expel", false},

		// authenticated web guest is not restricted
		{"web, authenticated, non-listed op", claimed, origin, true, "user.expel", false},

		// unclaimed: confined to the unclaimed list
		{"web, unclaimed, listed op", unclaimed, origin, false, "user.info", false},
		{"web, unclaimed, listed op with params", unclaimed, origin, false, "user.info?in=json", false},
		{"web, unclaimed, non-listed op", unclaimed, origin, false, "apphost.list_tokens", true},

		// claimed: confined to the claimed list - former setup ops now refused
		{"web, claimed, listed op", claimed, origin, false, "apphost.register", false},
		{"web, claimed, unclaimed-only op refused", claimed, origin, false, "user.info", true},
		{"web, claimed, non-listed op", claimed, origin, false, "apphost.list_tokens", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.mod.blocksAnonymousWeb(ctx, c.webOrigin, c.authenticated, q(c.query)); got != c.want {
				t.Fatalf("blocksAnonymousWeb = %v; want %v", got, c.want)
			}
		})
	}
}
