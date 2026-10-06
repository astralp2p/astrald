package objects

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestReadSeeker_AdoptsTheOpenedID starts a ReadSeeker from a partial ID with no reader.
// The first open adopts the reader's full ID, so a seek from the end uses the full size.
func TestReadSeeker_AdoptsTheOpenedID(t *testing.T) {
	data := []byte("a partial id names an object by its hash alone")
	hash := sha256.Sum256(data)
	repo := &stubRepository{reader: &stubReader{
		id:   astral.ObjectID{Size: uint64(len(data)), Hash: hash},
		data: bytes.NewReader(data),
	}}
	partial := &astral.ObjectID{Hash: hash}

	rs := NewReadSeeker(astral.NewContext(nil), partial, repo, nil)
	defer rs.Close()

	head := make([]byte, 4)
	if _, err := io.ReadFull(rs, head); err != nil {
		t.Fatalf("read the head: %v", err)
	}

	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("seek to the end: %v", err)
	}
	if end != int64(len(data)) {
		t.Fatalf("seek to the end landed at %d; want the full size %d", end, len(data))
	}
	if partial.Size != 0 {
		t.Fatalf("the ReadSeeker changed its argument's size to %d", partial.Size)
	}
}

// offsetRepository serves data from the requested offset and rejects an offset past the end.
// note: the embedded nil interface panics on any other method, which asserts that the caller uses only Read.
type offsetRepository struct {
	Repository
	id   astral.ObjectID
	data []byte
}

func (repo *offsetRepository) Read(_ *astral.Context, objectID *astral.ObjectID, offset, limit int64) (Reader, error) {
	n, err := ResolveReadLimit(&repo.id, offset, limit)
	if err != nil {
		return nil, err
	}
	return &stubReader{id: repo.id, data: bytes.NewReader(repo.data[offset : offset+n])}, nil
}

// TestReadSeeker_FailedSeekKeepsTheOffset reads the head, seeks past the end, and reads again.
// The failed Seek reports the previous offset, and the next Read continues from it.
func TestReadSeeker_FailedSeekKeepsTheOffset(t *testing.T) {
	data := []byte("0123456789")
	repo := &offsetRepository{id: astral.ObjectID{Size: uint64(len(data)), Hash: sha256.Sum256(data)}, data: data}
	reader, err := repo.Read(nil, &repo.id, 0, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	rs := NewReadSeeker(astral.NewContext(nil), &repo.id, repo, reader)
	defer rs.Close()

	head := make([]byte, 4)
	if _, err := io.ReadFull(rs, head); err != nil {
		t.Fatalf("read the head: %v", err)
	}

	pos, err := rs.Seek(20, io.SeekStart)
	if !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("seek past the end returned %v; want ErrOutOfBounds", err)
	}
	if pos != 4 {
		t.Fatalf("failed seek reported offset %d; want the previous offset 4", pos)
	}

	next := make([]byte, 4)
	if _, err := io.ReadFull(rs, next); err != nil {
		t.Fatalf("read after the failed seek: %v", err)
	}
	if string(next) != "4567" {
		t.Fatalf("read after the failed seek returned %q; want %q", next, "4567")
	}
}
