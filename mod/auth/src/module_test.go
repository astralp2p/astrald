package auth

import (
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

func markingHandler(name string, verdict bool, calls *[]string) authmod.TypedHandler {
	return authmod.Func[*testAction](func(_ *astral.Context, _ *testAction) bool {
		*calls = append(*calls, name)
		return verdict
	})
}

func TestAddKeepsRegistrationOrder(t *testing.T) {
	mod := &Module{}
	var calls []string
	mod.Add(markingHandler("h1", false, &calls))
	mod.Add(markingHandler("h2", false, &calls))

	handlers := mod.get("test.action")
	if len(handlers) != 2 {
		t.Fatalf("get(test.action) holds %d handlers, want 2", len(handlers))
	}

	ctx := astral.NewContext(nil)
	for _, h := range handlers {
		h.Authorize(ctx, action(astral.GenerateIdentity()))
	}

	if want := []string{"h1", "h2"}; !slices.Equal(calls, want) {
		t.Fatalf("handler order = %v, want %v", calls, want)
	}
}

func TestAuthorizeAllowsWhenEitherHandlerAllows(t *testing.T) {
	cases := []struct {
		name   string
		h1, h2 bool
		want   bool
	}{
		{"first allows", true, false, true},
		{"second allows", false, true, true},
		{"neither allows", false, false, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := testModule(t)
			var calls []string
			mod.Add(markingHandler("h1", c.h1, &calls))
			mod.Add(markingHandler("h2", c.h2, &calls))

			if got := mod.Authorize(astral.NewContext(nil), action(astral.GenerateIdentity())); got != c.want {
				t.Fatalf("Authorize = %v, want %v (handlers asked: %v)", got, c.want, calls)
			}
		})
	}
}

func TestAddKeysHandlersByActionType(t *testing.T) {
	mod := &Module{}
	mod.Add(
		authmod.Func[*testAction](func(*astral.Context, *testAction) bool { return true }),
		authmod.Func[*authmod.SudoAction](func(*astral.Context, *authmod.SudoAction) bool { return true }),
	)

	for _, actionType := range []string{"test.action", "mod.auth.sudo_action"} {
		if n := len(mod.get(actionType)); n != 1 {
			t.Errorf("get(%q) holds %d handlers, want 1", actionType, n)
		}
	}
}
