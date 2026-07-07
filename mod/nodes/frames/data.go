package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ Frame = &Data{}

// Data is a Frame for transporting data
type Data struct {
	Nonce   astral.Nonce
	Payload []byte
}

// astral:blueprint-ignore
func (frame *Data) ObjectType() string {
	return "nodes.frames.data"
}

func (frame *Data) ReadFrom(r io.Reader) (n int64, err error) {
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	var plen uint16
	err = binary.Read(r, astral.ByteOrder, &plen)
	if err != nil {
		return
	}
	n += 2

	frame.Payload = make([]byte, plen)
	var m int
	m, err = io.ReadFull(r, frame.Payload)
	n += int64(m)

	return
}

func (frame *Data) WriteTo(w io.Writer) (n int64, err error) {
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Write(w, astral.ByteOrder, uint16(len(frame.Payload)))
	if err != nil {
		return
	}
	n += 2

	var m int
	m, err = w.Write(frame.Payload)
	n += int64(m)

	return
}

func (frame *Data) String() string {
	return fmt.Sprintf("data(%s,[%d])", frame.Nonce.String(), len(frame.Payload))
}
