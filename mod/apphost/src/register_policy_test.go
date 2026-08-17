package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
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
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	askedGrant := parsePermits("mod.auth.serve_objects_action")
	askedContract := parsePermits("mod.nodes.relay_for_action")

	grantPermits, contractPermits, ok := mod.AppRegisterAcceptAll("", askedGrant, askedContract)
	if !ok {
		t.Fatal("the default policy refuses no registration")
	}
	if !sameActions(grantPermits, askedGrant) {
		t.Fatalf("granted %v, want %v", actions(grantPermits), actions(askedGrant))
	}
	if !sameActions(contractPermits, askedContract) {
		t.Fatalf("contracted %v, want %v", actions(contractPermits), actions(askedContract))
	}
}

// The rails do not leak into one another: a permit asked for on one is never
// written on the other, so an app cannot obtain a signature by asking for a
// grant, nor a revocable row by asking for a contract.
func TestTheDefaultPolicyKeepsTheRailsApart(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	grantPermits, contractPermits, _ := mod.AppRegisterAcceptAll(
		"", parsePermits("mod.auth.serve_objects_action"), nil,
	)
	if len(contractPermits) != 0 {
		t.Fatalf("a grant request reached the contract rail: %v", actions(contractPermits))
	}
	if len(grantPermits) != 1 {
		t.Fatalf("granted %v, want one permit", actions(grantPermits))
	}

	grantPermits, contractPermits, _ = mod.AppRegisterAcceptAll(
		"", nil, parsePermits("mod.nodes.relay_for_action"),
	)
	if len(grantPermits) != 0 {
		t.Fatalf("a contract request reached the grant rail: %v", actions(grantPermits))
	}
	if len(contractPermits) != 1 {
		t.Fatalf("contracted %v, want one permit", actions(contractPermits))
	}
}

// Assembling the contract request is the op's job, not the policy's: the policy
// is handed the origin's entitlement and the app's ask already joined. The
// entitlement joins the contract rail because a PermitConfig carries Delegation.
func TestTheOpJoinsEntitlementAndAsk(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	var origin string
	for o := range defaultConfig.TrustedWebSources {
		origin = o
		break
	}
	if origin == "" {
		t.Skip("no trusted web source configured by default")
	}

	entitled := mod.GetWebOriginPermits(origin)
	if len(entitled) == 0 {
		t.Fatal("a trusted origin is entitled to nothing")
	}

	joined := append(mod.GetWebOriginPermits(origin), parsePermits("app.asked_for_action")...)
	if len(joined) != len(entitled)+1 {
		t.Fatalf("joined %v, expected the entitlement plus one ask", actions(joined))
	}
}

var _ = astral.String8("")
