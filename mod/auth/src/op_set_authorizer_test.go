package auth

import (
	"fmt"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// setAuthorizer routes an auth.set_authorizer query for action from caller with
// the given origin (nil sets none) and returns the op's one answer.
func setAuthorizer(t *testing.T, mod *Module, caller *astral.Identity, origin any, action string) astral.Object {
	t.Helper()
	op, err := routing.NewOp(mod.OpSetAuthorizer)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	q := astral.Launch(query.New(caller, mod.node.Identity(), "auth.set_authorizer", map[string]string{
		"action": action, "endpoint": "http://authz/authorize",
	}))
	if origin != nil {
		q.Extra.Set("origin", origin)
	}
	out, outWriter := io.Pipe()
	in, err := op.RouteQuery(astral.NewContext(nil), q, outWriter)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	defer in.Close()
	defer out.Close()
	obj, err := channel.NewReceiver(out).Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	return obj
}

// TestSetAuthorizerAdmitsALocalOriginOnly: a networked or MCP query is refused
// before anything is asked, even from a holder of AdminManageApps, and installs
// no authority; a local one is admitted.
func TestSetAuthorizerAdmitsALocalOriginOnly(t *testing.T) {
	for _, tc := range []struct {
		origin  any
		refused bool
	}{
		{nil, false},
		{astral.OriginLocal, false},
		{astral.OriginNetwork, true},
		{astral.OriginMCP, true},
	} {
		r := newSetAuthorizerRig(t)
		got := setAuthorizer(t, r.mod, r.holder, tc.origin, "test.action")
		want := "ack"
		if tc.refused {
			want = astral.NewErrOriginNotAllowed(fmt.Sprint(tc.origin)).Error()
		}
		if answer(got) != want {
			t.Fatalf("origin %v: answered %v; want %q", tc.origin, got, want)
		}
		if _, ok := r.mod.external.Get("test.action"); ok == tc.refused {
			t.Fatalf("origin %v: authority installed %v; want %v", tc.origin, ok, !tc.refused)
		}
	}
}

// TestSetAuthorizerAsksAdminManageAppsAlone: a local holder of AdminManageApps
// installs an authority for any action, a user power included; a caller
// without it installs none.
func TestSetAuthorizerAsksAdminManageAppsAlone(t *testing.T) {
	for _, action := range []string{
		auth.SeeObjectsAction{}.ObjectType(), auth.SudoAction{}.ObjectType(),
		user.ExpelAction{}.ObjectType(), nodes.RelayForAction{}.ObjectType(),
	} {
		r := newSetAuthorizerRig(t)
		if got := setAuthorizer(t, r.mod, r.holder, nil, action); answer(got) != "ack" {
			t.Fatalf("%s by the holder: answered %v; want ack", action, got)
		}
		if _, ok := r.mod.external.Get(action); !ok {
			t.Fatalf("%s by the holder: no authority installed", action)
		}

		r = newSetAuthorizerRig(t)
		if got := setAuthorizer(t, r.mod, astral.GenerateIdentity(), nil, action); answer(got) != auth.ErrNotPermitted.Error() {
			t.Fatalf("%s by an outsider: answered %v; want %v", action, got, auth.ErrNotPermitted)
		}
		if _, ok := r.mod.external.Get(action); ok {
			t.Fatalf("%s by an outsider: the authority was installed", action)
		}
	}
}

type setAuthorizerRig struct {
	mod    *Module
	holder *astral.Identity
}

// newSetAuthorizerRig is a node where holder alone holds AdminManageApps.
func newSetAuthorizerRig(t *testing.T) *setAuthorizerRig {
	t.Helper()
	r := &setAuthorizerRig{holder: astral.GenerateIdentity()}
	r.mod = boundModule(t, &leafNode{}, nil)
	r.mod.node = &identityNode{id: astral.GenerateIdentity()}
	r.mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(_ *astral.Context, a *auth.AdminManageAppsAction) bool {
		return a.Actor().IsEqual(r.holder)
	}))
	return r
}

// answer names an op's answer: "ack" or the error text.
func answer(obj astral.Object) string {
	switch o := obj.(type) {
	case *astral.Ack:
		return "ack"
	case astral.Error:
		return o.Error()
	}
	return fmt.Sprintf("%T", obj)
}
