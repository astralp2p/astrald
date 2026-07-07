package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ Frame = &Ping{}

type Ping struct {
	Nonce astral.Nonce
	Pong  bool
}

// astral:blueprint-ignore
func (frame *Ping) ObjectType() string {
	return "nodes.frames.ping"
}

func (frame *Ping) ReadFrom(r io.Reader) (n int64, err error) {
	// opcode is handled by Blueprints; just read the payload
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Read(r, astral.ByteOrder, &frame.Pong)
	if err != nil {
		return
	}
	n += 1

	return
}

func (frame *Ping) WriteTo(w io.Writer) (n int64, err error) {
	// Blueprints.Encode writes the type; just write the payload
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Write(w, astral.ByteOrder, frame.Pong)
	if err != nil {
		return
	}
	n += 1

	return
}

func (frame *Ping) String() string {
	if frame.Pong {
		return fmt.Sprintf("pong(%s)", frame.Nonce)
	}
	return fmt.Sprintf("ping(%s)", frame.Nonce)
}
