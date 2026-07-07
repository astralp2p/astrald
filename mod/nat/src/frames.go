package nat

import (
	"encoding/binary"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type pingFrame struct {
	Nonce astral.Nonce
	Pong  bool
}

func (f *pingFrame) WriteTo(w io.Writer) (n int64, err error) {
	var written int64
	if err := binary.Write(w, astral.ByteOrder, f.Nonce); err != nil {
		return written, err
	}
	written += 8
	var pongByte byte
	if f.Pong {
		pongByte = 1
	}
	if err := binary.Write(w, astral.ByteOrder, pongByte); err != nil {
		return written, err
	}
	written++
	return written, nil
}

func (f *pingFrame) ReadFrom(r io.Reader) (n int64, err error) {
	var read int64
	if err := binary.Read(r, astral.ByteOrder, &f.Nonce); err != nil {
		return read, err
	}
	read += 8
	var pongByte byte
	if err := binary.Read(r, astral.ByteOrder, &pongByte); err != nil {
		return read, err
	}
	f.Pong = pongByte == 1
	read++
	return read, nil
}
