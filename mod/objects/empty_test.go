package objects

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestIsEmptyObject: only the ID of empty content names the empty object; other size-0 IDs are partial IDs.
func TestIsEmptyObject(t *testing.T) {
	resolved, err := astral.Resolve(bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	hello, err := astral.Resolve(bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		id   *astral.ObjectID
		want bool
	}{
		{"resolved empty content", resolved, true},
		{"EmptyObjectID", EmptyObjectID(), true},
		{"partial ID of hello", &astral.ObjectID{Hash: hello.Hash}, false},
		{"full ID of hello", hello, false},
		{"zero ID", &astral.ObjectID{}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := IsEmptyObject(c.id); got != c.want {
			t.Errorf("%v: IsEmptyObject = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestEmptyObjectIDIsACopy: a caller that changes the returned ID does not change the next one.
func TestEmptyObjectIDIsACopy(t *testing.T) {
	EmptyObjectID().Size = 1

	if !IsEmptyObject(EmptyObjectID()) {
		t.Fatal("EmptyObjectID changed after a caller wrote to a returned copy")
	}
}

// TestOpenEmptyObject: the reader answers zero bytes, names the empty object and the repository; offset past 0 is out of bounds.
func TestOpenEmptyObject(t *testing.T) {
	var repo Repository

	r, err := OpenEmptyObject(repo, 0, 10)
	if err != nil {
		t.Fatalf("OpenEmptyObject: %v", err)
	}
	defer r.Close()

	if data, err := io.ReadAll(r); err != nil || len(data) != 0 {
		t.Errorf("ReadAll = %q, %v, want no bytes", data, err)
	}
	if !IsEmptyObject(r.ID()) {
		t.Errorf("ID() = %v, want the empty ObjectID", r.ID())
	}
	if r.Repo() != repo {
		t.Errorf("Repo() = %v, want %v", r.Repo(), repo)
	}
	if _, ok := r.(io.Seeker); !ok {
		t.Error("the reader cannot seek")
	}

	if _, err := OpenEmptyObject(repo, 1, 0); !errors.Is(err, ErrOutOfBounds) {
		t.Errorf("OpenEmptyObject at offset 1 = %v, want ErrOutOfBounds", err)
	}
}
