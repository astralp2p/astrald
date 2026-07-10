package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ Frame = &Query{}

type Query struct {
	Nonce  astral.Nonce
	Buffer uint32
	Query  string
}

// astral:blueprint-ignore
func (frame *Query) ObjectType() string {
	return "nodes.frames.query"
}

func (frame *Query) ReadFrom(r io.Reader) (n int64, err error) {
	// opcode is handled by Blueprints; just read payload
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Read(r, astral.ByteOrder, &frame.Buffer)
	if err != nil {
		return
	}
	n += 4

	var plen uint16

	err = binary.Read(r, astral.ByteOrder, &plen)
	if err != nil {
		return
	}
	n += 2

	var b = make([]byte, plen)
	var m int
	m, err = io.ReadFull(r, b)
	n += int64(m)
	frame.Query = string(b[:m])

	return
}

func (frame *Query) WriteTo(w io.Writer) (n int64, err error) {
	// Blueprints.Encode writes the type; just write the payload
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Write(w, astral.ByteOrder, frame.Buffer)
	if err != nil {
		return
	}
	n += 4

	var plen = uint16(len(frame.Query))
	err = binary.Write(w, astral.ByteOrder, plen)
	if err != nil {
		return
	}
	n += 2

	var m int
	m, err = w.Write([]byte(frame.Query))
	n += int64(m)

	return
}

func (frame *Query) String() string {
	return fmt.Sprintf("query(%s, '%s')", frame.Nonce.String(), frame.Query)
}
