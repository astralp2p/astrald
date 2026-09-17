package auth

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func countingSudo(verdict bool) (Func[*SudoAction], *int) {
	calls := new(int)
	return Func[*SudoAction](func(_ *astral.Context, _ *SudoAction) bool {
		*calls++
		return verdict
	}), calls
}

func TestFuncActionType(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"SudoAction", Func[*SudoAction](nil).ActionType(), "mod.auth.sudo_action"},
		{"AdminNetworkAction", Func[*auth.AdminNetworkAction](nil).ActionType(), auth.AdminNetworkAction{}.ObjectType()},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: ActionType() = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestFuncAuthorizeRefusesAnotherActionType(t *testing.T) {
	f, calls := countingSudo(true)
	other := &auth.AdminNetworkAction{Action: auth.NewAction(astral.GenerateIdentity())}

	if f.Authorize(astral.NewContext(nil), other) {
		t.Fatal("Authorize(AdminNetworkAction) = true, want false")
	}
	if *calls != 0 {
		t.Fatalf("wrapped func called %d times for another action type, want 0", *calls)
	}
}

func TestFuncAuthorizeReturnsTheFuncResult(t *testing.T) {
	for _, verdict := range []bool{true, false} {
		f, calls := countingSudo(verdict)
		id := astral.GenerateIdentity()
		action := &SudoAction{Action: auth.NewAction(id), AsID: id}

		if got := f.Authorize(astral.NewContext(nil), action); got != verdict {
			t.Errorf("Authorize(SudoAction) = %v, want %v", got, verdict)
		}
		if *calls != 1 {
			t.Errorf("wrapped func called %d times, want 1", *calls)
		}
	}
}

func TestNodeLocalMarksAndDelegates(t *testing.T) {
	f, calls := countingSudo(true)

	var plain TypedHandler = f
	if _, ok := plain.(interface{ NodeLocal() }); ok {
		t.Fatal("a plain Func carries the NodeLocal marker")
	}

	local := NodeLocal(f)
	if _, ok := local.(interface{ NodeLocal() }); !ok {
		t.Fatal("NodeLocal(f) does not carry the NodeLocal marker")
	}

	if got, want := local.ActionType(), f.ActionType(); got != want {
		t.Fatalf("NodeLocal(f).ActionType() = %q, want %q", got, want)
	}

	ctx := astral.NewContext(nil)
	id := astral.GenerateIdentity()
	if !local.Authorize(ctx, &SudoAction{Action: auth.NewAction(id), AsID: id}) {
		t.Fatal("NodeLocal(f).Authorize(SudoAction) = false, want the wrapped func's true")
	}
	if *calls != 1 {
		t.Fatalf("wrapped func called %d times, want 1", *calls)
	}

	if local.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(id)}) {
		t.Fatal("NodeLocal(f).Authorize(AdminNetworkAction) = true, want false")
	}
	if *calls != 1 {
		t.Fatalf("wrapped func called %d times after another action type, want 1", *calls)
	}
}
