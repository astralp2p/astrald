package frames

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ Frame = &Response{}

const (
	CodeAccepted = iota
	CodeRejected
)

type Response struct {
	Nonce   astral.Nonce
	ErrCode uint8
	Buffer  uint32
}

// astral:blueprint-ignore
func (frame *Response) ObjectType() string {
	return "nodes.frames.response"
}

func (frame *Response) ReadFrom(r io.Reader) (n int64, err error) {
	err = binary.Read(r, astral.ByteOrder, &frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Read(r, astral.ByteOrder, &frame.ErrCode)
	if err != nil {
		return
	}
	n += 1

	err = binary.Read(r, astral.ByteOrder, &frame.Buffer)
	if err != nil {
		return
	}
	n += 4

	return
}

func (frame *Response) WriteTo(w io.Writer) (n int64, err error) {
	err = binary.Write(w, astral.ByteOrder, frame.Nonce)
	if err != nil {
		return
	}
	n += 8

	err = binary.Write(w, astral.ByteOrder, frame.ErrCode)
	if err != nil {
		return
	}
	n += 1

	err = binary.Write(w, astral.ByteOrder, frame.Buffer)
	if err != nil {
		return
	}
	n += 4

	return
}

func (frame *Response) String() string {
	return fmt.Sprintf("response(%s, %d)", frame.Nonce.String(), frame.ErrCode)
}
