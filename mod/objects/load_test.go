package objects

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// stubRepository answers Read with its reader and counts the calls.
// note: the embedded nil interface panics on any other method, which asserts that the caller uses only Read.
type stubRepository struct {
	Repository
	reader *stubReader
	reads  atomic.Uint64
}

func (repo *stubRepository) Read(*astral.Context, *astral.ObjectID, int64, int64) (Reader, error) {
	repo.reads.Add(1)
	return repo.reader, nil
}

// stubReader serves data and reports id as the full ID of the object it opened.
type stubReader struct {
	id     astral.ObjectID
	data   *bytes.Reader
	served atomic.Uint64
	closed atomic.Bool
}

func (r *stubReader) Read(p []byte) (int, error) {
	n, err := r.data.Read(p)
	r.served.Add(uint64(n))
	return n, err
}

func (r *stubReader) Close() error {
	r.closed.Store(true)
	return nil
}

func (r *stubReader) Repo() Repository { return nil }

func (r *stubReader) ID() *astral.ObjectID {
	id := r.id
	return &id
}

// loadStub returns a repository whose reader serves a canonical String8 and reports size as the object's size.
func loadStub(t *testing.T, size uint64) (*stubRepository, *astral.ObjectID) {
	t.Helper()

	data, err := astral.EncodeBytes(astral.NewString8("stub"), astral.Canonical())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	hash := sha256.Sum256(data)
	reader := &stubReader{id: astral.ObjectID{Size: size, Hash: hash}, data: bytes.NewReader(data)}

	return &stubRepository{reader: reader}, &astral.ObjectID{Hash: hash}
}

// TestLoad_RefusesAResolvedSizeAboveTheCap drives the check after lookup: a partial
// argument passes the input check, and the reader's resolved size is refused before decoding.
func TestLoad_RefusesAResolvedSizeAboveTheCap(t *testing.T) {
	repo, partial := loadStub(t, uint64(MaxObjectSize)+1)

	_, err := Load[*astral.String8](nil, repo, partial)
	if !errors.Is(err, ErrObjectTooLarge) {
		t.Fatalf("Load of a resolved size %d: got err %v, want %v", uint64(MaxObjectSize)+1, err, ErrObjectTooLarge)
	}

	if n := repo.reads.Load(); n != 1 {
		t.Fatalf("Load opened the object %d times; want 1", n)
	}
	if n := repo.reader.served.Load(); n != 0 {
		t.Fatalf("Load read %d bytes of an object above the cap; want 0", n)
	}
	if !repo.reader.closed.Load() {
		t.Fatal("Load left the reader of a refused object open")
	}
	if partial.Size != 0 {
		t.Fatalf("Load changed the argument's size to %d", partial.Size)
	}
}

// TestLoad_DecodesAResolvedSizeAtTheCap pins the boundary: a resolved size equal to
// MaxObjectSize is decoded.
func TestLoad_DecodesAResolvedSizeAtTheCap(t *testing.T) {
	repo, partial := loadStub(t, uint64(MaxObjectSize))

	o, err := Load[*astral.String8](nil, repo, partial)
	if err != nil {
		t.Fatalf("Load of a resolved size %d: %v", MaxObjectSize, err)
	}

	if o.String() != "stub" {
		t.Fatalf("Load decoded %q; want %q", o.String(), "stub")
	}
	if !repo.reader.closed.Load() {
		t.Fatal("Load left the reader open")
	}
}

// TestLoad_RefusesAnInputSizeAboveTheCap keeps the early check: a full argument above
// the cap is refused before any lookup, also at a size int64 cannot hold.
func TestLoad_RefusesAnInputSizeAboveTheCap(t *testing.T) {
	for _, size := range []uint64{uint64(MaxObjectSize) + 1, 1 << 63} {
		t.Run(strconv.FormatUint(size, 10), func(t *testing.T) {
			repo, partial := loadStub(t, 1)
			full := &astral.ObjectID{Size: size, Hash: partial.Hash}

			_, err := Load[*astral.String8](nil, repo, full)
			if !errors.Is(err, ErrObjectTooLarge) {
				t.Fatalf("Load of an input size %d: got err %v, want %v", full.Size, err, ErrObjectTooLarge)
			}

			if n := repo.reads.Load(); n != 0 {
				t.Fatalf("Load opened the object %d times; want 0", n)
			}
		})
	}
}

type sourceIdentifier struct {
	id *astral.Identity
}

func (s sourceIdentifier) SourceIdentity() *astral.Identity { return s.id }

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

func TestLoad_WrongType(t *testing.T) {
	repo, id := loadStub(t, 1)

	_, err := Load[*astral.Uint8](nil, repo, id)
	if want := "cannot cast *astral.String8 into *astral.Uint8"; err == nil || err.Error() != want {
		t.Fatalf("Load[*astral.Uint8]: got %v, want %q", err, want)
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
