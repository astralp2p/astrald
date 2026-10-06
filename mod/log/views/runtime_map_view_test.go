package views

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestLessMapKey(t *testing.T) {
	for _, c := range []struct {
		a, b any
		want bool
	}{
		{uint64(2), uint64(10), true},
		{uint64(10), uint64(2), false},
		{"10", "2", true},
	} {
		if got := lessMapKey(c.a, c.b); got != c.want {
			t.Errorf("lessMapKey(%#v, %#v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestRuntimeMapViewSortsNumericKeys(t *testing.T) {
	m, err := astral.NewRuntimeMap("uint32", "string8")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []uint64{10, 2, 33} {
		if err := m.Set(k, astral.NewString8("v")); err != nil {
			t.Fatalf("Set(%d): %v", k, err)
		}
	}

	const want = "map[2: v, 10: v, 33: v]"
	for i := range 5 {
		if got := render(m); got != want {
			t.Fatalf("render #%d = %q, want %q", i, got, want)
		}
	}
}
