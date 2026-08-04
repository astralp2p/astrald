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

// The shipped default is permissive by name: it grants what it is handed,
// entitlement and ask alike, and leaves deciding to a policy that decides.
func TestTheDefaultPolicyGrantsWhatItIsHanded(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	asked := parsePermits("mod.user.info_action")

	granted, ok := mod.AppRegisterAcceptAll("", asked)
	if !ok {
		t.Fatal("the default policy refuses no registration")
	}
	if len(granted) != len(asked) {
		t.Fatalf("granted %v, want %v", actions(granted), actions(asked))
	}
}

// Assembling the list is the op's job, not the policy's: the policy is handed
// the origin's entitlement and the app's ask already joined.
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
