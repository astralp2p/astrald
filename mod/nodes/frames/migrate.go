package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ Frame = &Migrate{}

type Migrate struct {
	Nonce astral.Nonce
}

// astral:blueprint-ignore
func (frame *Migrate) ObjectType() string {
	return "nodes.frames.migrate"
}

func (frame *Migrate) ReadFrom(r io.Reader) (n int64, err error) {
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	return
}

func (frame *Migrate) WriteTo(w io.Writer) (n int64, err error) {
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	return
}

func (frame *Migrate) String() string {
	return fmt.Sprintf("migrate(%s)", frame.Nonce)
}
