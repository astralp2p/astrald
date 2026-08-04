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

// The default policy must not hand an app what it asked for. A stock node that
// granted on request would let any local app write itself a user.info permit.
func TestTheDefaultPolicyGrantsNothingItWasAskedFor(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	asked := parsePermits("mod.user.info_action")

	granted, ok := mod.AppRegisterAcceptAll("", asked)
	if !ok {
		t.Fatal("the default policy refuses no registration")
	}
	if len(granted) != 0 {
		t.Fatalf("granted %v to an IPC app that merely asked", actions(granted))
	}
}

// ...while an origin the node already trusts keeps its entitlement, whatever
// the app did or did not ask for.
func TestATrustedOriginKeepsItsEntitlement(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	// read the trusted origin from the config rather than restating it, so the
	// test follows the node's own list instead of a copy that goes stale
	var origin string
	for o := range defaultConfig.TrustedWebSources {
		origin = o
		break
	}
	if origin == "" {
		t.Skip("no trusted web source configured by default")
	}

	granted, ok := mod.AppRegisterAcceptAll(origin, nil)
	if !ok {
		t.Fatal("the default policy refuses no registration")
	}
	if len(granted) == 0 {
		t.Fatalf("a trusted origin lost its permits")
	}

	var found bool
	for _, a := range actions(granted) {
		if a == "mod.user.info_action" {
			found = true
		}
	}
	if !found {
		t.Fatalf("granted %v, expected the origin's user.info entitlement", actions(granted))
	}
}

var _ = astral.String8("")
