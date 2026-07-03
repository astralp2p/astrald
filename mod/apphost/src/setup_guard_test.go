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

func TestIsSetupOp(t *testing.T) {
	for _, op := range []string{"user.info", "tree.set", "coldcard.scan"} {
		if !isSetupOp(op) {
			t.Errorf("%q should be a setup op", op)
		}
	}
	for _, op := range []string{"apphost.list_tokens", "user.adopt", "apphost.create_token", "apphost.register"} {
		if isSetupOp(op) {
			t.Errorf("%q should not be a setup op", op)
		}
	}
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

func TestSetupModeBlocks(t *testing.T) {
	ctx := astral.NewContext(nil)

	noUser := &Module{}
	noUser.User = &stubUser{id: nil, ready: closedChan()}

	hasUser := &Module{}
	hasUser.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}

	q := func(s string) *astral.Query { return &astral.Query{QueryString: s} }

	cases := []struct {
		name      string
		mod       *Module
		webOrigin string
		query     string
		want      bool
	}{
		{"IPC guest (no origin), no user, non-setup op", noUser, "", "apphost.list_tokens", false},
		{"web guest, has user", hasUser, "https://settings.astrald.app", "apphost.list_tokens", false},
		{"web guest, no user, setup op", noUser, "https://settings.astrald.app", "user.info", false},
		{"web guest, no user, setup op with params", noUser, "https://settings.astrald.app", "user.info?in=json", false},
		{"web guest, no user, non-setup op", noUser, "https://settings.astrald.app", "apphost.list_tokens", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.mod.setupModeBlocks(ctx, c.webOrigin, q(c.query)); got != c.want {
				t.Fatalf("setupModeBlocks = %v; want %v", got, c.want)
			}
		})
	}
}
