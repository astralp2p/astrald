package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ Frame = &Reset{}

type Reset struct {
	Nonce astral.Nonce
}

// astral:blueprint-ignore
func (frame *Reset) ObjectType() string {
	return "nodes.frames.reset"
}

func (frame *Reset) ReadFrom(r io.Reader) (n int64, err error) {
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	return
}

func (frame *Reset) WriteTo(w io.Writer) (n int64, err error) {
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	return
}

func (frame *Reset) String() string {
	return fmt.Sprintf("reset(%s)", frame.Nonce)
}
