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

// TestSetAuthorizerAdmitsALocalOriginOnly: a networked query is refused before
// anything is asked, even from the user; a local one is admitted.
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
		got := setAuthorizer(t, r.mod, r.user, tc.origin, "test.action")
		want := "ack"
		if tc.refused {
			want = astral.NewErrOriginNotAllowed(fmt.Sprint(tc.origin)).Error()
		}
		if answer(got) != want {
			t.Fatalf("origin %v: answered %v; want %q", tc.origin, got, want)
		}
	}
}

// TestSetAuthorizerLeavesUserAuthorityToTheUser: a caller holding
// AdminManageApps that is not the user installs no authority for a sudo, an
// expel or a relay; the user does, and the caller still installs one for any
// other action.
func TestSetAuthorizerLeavesUserAuthorityToTheUser(t *testing.T) {
	for _, action := range []string{
		auth.SudoAction{}.ObjectType(), user.ExpelAction{}.ObjectType(), nodes.RelayForAction{}.ObjectType(),
	} {
		r := newSetAuthorizerRig(t)
		if got := setAuthorizer(t, r.mod, r.privileged, nil, action); answer(got) != auth.ErrNotPermitted.Error() {
			t.Fatalf("%s by a node: answered %v; want %v", action, got, auth.ErrNotPermitted)
		}
		if _, ok := r.mod.external.Get(action); ok {
			t.Fatalf("%s by a node: the authority was installed", action)
		}
		if got := setAuthorizer(t, r.mod, r.user, nil, action); answer(got) != "ack" {
			t.Fatalf("%s by the user: answered %v; want ack", action, got)
		}
	}

	r := newSetAuthorizerRig(t)
	if got := setAuthorizer(t, r.mod, r.privileged, nil, auth.SeeObjectsAction{}.ObjectType()); answer(got) != "ack" {
		t.Fatalf("see_objects by a node: answered %v; want ack", got)
	}
}

type setAuthorizerRig struct {
	mod              *Module
	user, privileged *astral.Identity
}

// newSetAuthorizerRig is a claimed node where the user and a Privileged node
// both hold AdminManageApps.
func newSetAuthorizerRig(t *testing.T) *setAuthorizerRig {
	t.Helper()
	r := &setAuthorizerRig{user: astral.GenerateIdentity(), privileged: astral.GenerateIdentity()}
	r.mod = boundModule(t, &leafNode{}, nil)
	r.mod.node = &identityNode{id: astral.GenerateIdentity()}
	r.mod.User = readyClaim(r.user)
	r.mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(_ *astral.Context, a *auth.AdminManageAppsAction) bool {
		return a.Actor().IsEqual(r.user) || a.Actor().IsEqual(r.privileged)
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
