package objects

import (
	"bytes"
	"crypto/sha256"
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
