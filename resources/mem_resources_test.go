package resources

import (
	"errors"
	"testing"
)

func TestMemResourcesReadWrite(t *testing.T) {
	res := NewMemResources()

	data, err := res.Read("x")
	if data != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(x) before Write = (%q, %v), want (nil, %v)", data, err, ErrNotFound)
	}

	for _, want := range []string{"b1", "b2"} {
		if err := res.Write("x", []byte(want)); err != nil {
			t.Fatalf("Write(x, %q): %v", want, err)
		}
		data, err := res.Read("x")
		if err != nil || string(data) != want {
			t.Fatalf("Read(x) = (%q, %v), want (%q, nil)", data, err, want)
		}
	}
}
