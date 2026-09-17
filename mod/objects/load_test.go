package objects

import (
	"bytes"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// why: mod/objects/mem imports this package, so an in-package test cannot build a mem repository.
type bytesRepository struct {
	Repository
	data  []byte
	reads int
}

func (repo *bytesRepository) Read(*astral.Context, *astral.ObjectID, int64, int64) (Reader, error) {
	repo.reads++
	return &bytesReader{Reader: bytes.NewReader(repo.data), repo: repo}, nil
}

type bytesReader struct {
	*bytes.Reader
	repo Repository
}

func (r *bytesReader) Close() error { return nil }

func (r *bytesReader) Repo() Repository { return r.repo }

type sourceIdentifier struct {
	id *astral.Identity
}

func (s sourceIdentifier) SourceIdentity() *astral.Identity { return s.id }

func storedString8(t *testing.T, s string) (*bytesRepository, *astral.ObjectID) {
	t.Helper()

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, astral.NewString8(s), astral.Canonical()); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	id, err := astral.Resolve(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return &bytesRepository{data: buf.Bytes()}, id
}

func TestIsOffsetLimitValid(t *testing.T) {
	id := &astral.ObjectID{Size: 10}

	for _, tc := range []struct {
		offset, limit int64
		want          bool
	}{
		{0, 0, true},
		{0, 10, true},
		{10, 0, true},
		{5, 5, true},
		{5, 6, false},
		{11, 0, false},
		{-1, 0, false},
		{0, -1, false},
	} {
		if got := IsOffsetLimitValid(id, tc.offset, tc.limit); got != tc.want {
			t.Errorf("IsOffsetLimitValid(size 10, %d, %d): got %v, want %v", tc.offset, tc.limit, got, tc.want)
		}
	}
}

func TestLoad_DecodesStoredType(t *testing.T) {
	repo, id := storedString8(t, "hello")

	got, err := Load[*astral.String8](astral.NewContext(nil), repo, id)
	if err != nil {
		t.Fatalf("Load[*astral.String8]: %v", err)
	}
	if got == nil || string(*got) != "hello" {
		t.Fatalf("Load[*astral.String8]: got %v, want \"hello\"", got)
	}
}

func TestLoad_WrongType(t *testing.T) {
	repo, id := storedString8(t, "hello")

	_, err := Load[*astral.Uint8](astral.NewContext(nil), repo, id)
	if want := "cannot cast *astral.String8 into *astral.Uint8"; err == nil || err.Error() != want {
		t.Fatalf("Load[*astral.Uint8]: got %v, want %q", err, want)
	}
}

func TestLoad_TooLarge(t *testing.T) {
	repo := &bytesRepository{}
	id := &astral.ObjectID{Size: uint64(MaxObjectSize) + 1}

	_, err := Load[*astral.String8](astral.NewContext(nil), repo, id)
	if !errors.Is(err, ErrObjectTooLarge) {
		t.Errorf("Load of MaxObjectSize+1: got %v, want %v", err, ErrObjectTooLarge)
	}
	if repo.reads != 0 {
		t.Errorf("Read calls: got %d, want 0", repo.reads)
	}
}

func TestSourceIdentity(t *testing.T) {
	valid := astral.GenerateIdentity()

	for _, tc := range []struct {
		name    string
		v       any
		wantID  *astral.Identity
		wantOK  bool
		wantErr error
	}{
		{name: "not a source identifier", v: 42},
		{name: "nil identity", v: sourceIdentifier{}, wantOK: true, wantErr: ErrInvalidSourceIdentity},
		{name: "zero identity", v: sourceIdentifier{id: &astral.Identity{}}, wantOK: true, wantErr: ErrInvalidSourceIdentity},
		{name: "valid identity", v: sourceIdentifier{id: valid}, wantID: valid, wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, ok, err := SourceIdentity(tc.v)

			if ok != tc.wantOK {
				t.Errorf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err: got %v, want %v", err, tc.wantErr)
			}
			if id != tc.wantID {
				t.Errorf("id: got %v, want %v", id, tc.wantID)
			}
		})
	}
}
