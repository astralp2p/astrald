package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

func actions(permits []*auth.Permit) (out []string) {
	for _, p := range permits {
		out = append(out, string(p.Action))
	}
	return
}

func sameActions(a, b []*auth.Permit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Action != b[i].Action {
			return false
		}
	}
	return true
}

// The shipped default is permissive by name: it writes what it is handed,
// and leaves deciding to a policy that decides.
func TestTheDefaultPolicyWritesWhatItIsHanded(t *testing.T) {
	askedGrant := permits("mod.auth.serve_objects_action")
	askedContract := permits("mod.nodes.relay_for_action")

	dec, err := apphostmod.AcceptAll(astral.NewContext(nil), &apphost.AppRegisterRequest{
		GrantPermits:    askedGrant,
		ContractPermits: askedContract,
	})
	if err != nil || !dec.Allow {
		t.Fatalf("the default policy refuses no registration: %v", err)
	}
	if !sameActions(dec.GrantPermits, askedGrant) {
		t.Fatalf("granted %v, want %v", actions(dec.GrantPermits), actions(askedGrant))
	}
	if !sameActions(dec.ContractPermits, askedContract) {
		t.Fatalf("contracted %v, want %v", actions(dec.ContractPermits), actions(askedContract))
	}
	if len(dec.Authorizers) != 0 {
		t.Fatalf("the default policy names %d authorizers", len(dec.Authorizers))
	}
}

var _ = astral.String8("")
