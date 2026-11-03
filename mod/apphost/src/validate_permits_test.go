package apphost

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// TestValidatePermits: the first invalid permit of a list is reported as an
// invalid permit naming its action; a discovery permit needs exactly one scope
// that names a service; constraints on an action that ignores them pass as
// sent.
func TestValidatePermits(t *testing.T) {
	discovery := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())
	twoScopes := discoveryPermit("inference")
	if err := twoScopes.Constraints.Append(discoveryPermit("chat").Constraints.Objects()[0]); err != nil {
		t.Fatalf("scope: %v", err)
	}
	shell := &auth.Permit{Action: "mod.shell.shell_action", Constraints: discoveryPermit("x").Constraints}

	for _, tc := range []struct {
		name    string
		permits []*auth.Permit
		invalid string
	}{
		{"empty list", nil, ""},
		{"unconstrained serve_objects", permits(auth.ServeObjectsAction{}.ObjectType()), ""},
		{"one discovery scope", []*auth.Permit{discoveryPermit("inference")}, ""},
		{"constrained shell action", []*auth.Permit{shell}, ""},
		{"unknown action", permits(auth.ServeObjectsAction{}.ObjectType(), "mod.nope.no_action", string(discovery)), "mod.nope.no_action"},
		{"discovery without constraints", permits(string(discovery)), string(discovery)},
		{"discovery with an empty bundle", []*auth.Permit{{Action: discovery, Constraints: astral.NewBundle()}}, string(discovery)},
		{"discovery with two scopes", []*auth.Permit{twoScopes}, string(discovery)},
		{"discovery scope without services", []*auth.Permit{discoveryPermit()}, string(discovery)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePermits(tc.permits)
			if tc.invalid == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if !errors.Is(err, auth.ErrInvalidPermit) {
				t.Fatalf("answered %v; want an invalid permit", err)
			}
			if want := "invalid permit: " + tc.invalid + ": "; !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("answered %q; want %q…", err, want)
			}
		})
	}
}

// TestRefuseRemovedArgs: a query naming a removed argument is refused with
// unknown argument naming it; other arguments pass.
func TestRefuseRemovedArgs(t *testing.T) {
	err := refuseRemovedArgs("apphost.grant?out=json&action=x", "identity", "action")
	if !errors.Is(err, query.ErrUnknownArgument) || err.Error() != "unknown argument: action" {
		t.Fatalf("answered %v; want unknown argument: action", err)
	}
	if err := refuseRemovedArgs("apphost.grant?in=json&out=json", "identity", "action"); err != nil {
		t.Fatalf("refused a query without removed arguments: %v", err)
	}
}
