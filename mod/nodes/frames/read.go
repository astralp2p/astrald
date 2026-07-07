package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ Frame = &Read{}

// Read is a frame requesting more data for the nonce
type Read struct {
	Nonce astral.Nonce
	Len   uint32
}

// astral:blueprint-ignore
func (frame *Read) ObjectType() string {
	return "nodes.frames.read"
}

func (frame *Read) ReadFrom(r io.Reader) (n int64, err error) {
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Read(r, astral.ByteOrder, &frame.Len)
	if err != nil {
		return
	}
	n += 4

	return
}

func (frame *Read) WriteTo(w io.Writer) (n int64, err error) {
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Write(w, astral.ByteOrder, frame.Len)
	if err != nil {
		return
	}
	n += 4

	return
}

func (frame *Read) String() string {
	return fmt.Sprintf("read(%s,%d)", frame.Nonce.String(), frame.Len)
}
