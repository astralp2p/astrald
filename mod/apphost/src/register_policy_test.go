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

func TestParsePermitsReadsAnAskedForSet(t *testing.T) {
	got := actions(parsePermits(" mod.user.info_action , mod.nodes.relay_for_action ,, "))
	want := []string{"mod.user.info_action", "mod.nodes.relay_for_action"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParsePermitsOfNothingAsksForNothing(t *testing.T) {
	if got := parsePermits(""); len(got) != 0 {
		t.Fatalf("got %v, want no permits", actions(got))
	}
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
// entitlement and ask alike, and leaves deciding to a policy that decides.
func TestTheDefaultPolicyWritesWhatItIsHanded(t *testing.T) {
	askedGrant := parsePermits("mod.auth.serve_objects_action")
	askedContract := parsePermits("mod.nodes.relay_for_action")

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

// The rails do not leak into one another: a permit asked for on one is never
// written on the other, so an app cannot obtain a signature by asking for a
// grant, nor a revocable row by asking for a contract.
func TestTheDefaultPolicyKeepsTheRailsApart(t *testing.T) {
	ctx := astral.NewContext(nil)

	dec, _ := apphostmod.AcceptAll(ctx, &apphost.AppRegisterRequest{
		GrantPermits: parsePermits("mod.auth.serve_objects_action"),
	})
	if len(dec.ContractPermits) != 0 {
		t.Fatalf("a grant request reached the contract rail: %v", actions(dec.ContractPermits))
	}
	if len(dec.GrantPermits) != 1 {
		t.Fatalf("granted %v, want one permit", actions(dec.GrantPermits))
	}

	dec, _ = apphostmod.AcceptAll(ctx, &apphost.AppRegisterRequest{
		ContractPermits: parsePermits("mod.nodes.relay_for_action"),
	})
	if len(dec.GrantPermits) != 0 {
		t.Fatalf("a contract request reached the grant rail: %v", actions(dec.GrantPermits))
	}
	if len(dec.ContractPermits) != 1 {
		t.Fatalf("contracted %v, want one permit", actions(dec.ContractPermits))
	}
}

var _ = astral.String8("")
