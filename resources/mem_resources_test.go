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

// TestMemResourcesDataSharesNamespace pins that memory resources keep one
// namespace for config and data names.
func TestMemResourcesDataSharesNamespace(t *testing.T) {
	res := NewMemResources()

	if err := res.WriteData("x", []byte("d")); err != nil {
		t.Fatalf("WriteData(x): %v", err)
	}
	for name, read := range map[string]func(string) ([]byte, error){"Read": res.Read, "ReadData": res.ReadData} {
		data, err := read("x")
		if err != nil || string(data) != "d" {
			t.Fatalf("%s(x) = (%q, %v), want (%q, nil)", name, data, err, "d")
		}
	}
}
