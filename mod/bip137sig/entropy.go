package bip137sig

import (
	"encoding/binary"
	"encoding/hex"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = (*Entropy)(nil)

type Entropy []byte

func (Entropy) ObjectType() string {
	return "bip137sig.entropy"
}

func (e Entropy) WriteTo(w io.Writer) (n int64, err error) {
	l := uint8(len(e))

	if int(l) < MinEntropyBytes || int(l) > MaxEntropyBytes || int(l)%EntropyStepBytes != 0 {
		return n, ErrInvalidEntropyLength
	}

	if err = binary.Write(w, astral.ByteOrder, &l); err != nil {
		return
	}
	n += 1

	var m int
	m, err = w.Write(e)
	n += int64(m)
	return
}

func (e *Entropy) ReadFrom(r io.Reader) (n int64, err error) {
	var l uint8
	if err = binary.Read(r, astral.ByteOrder, &l); err != nil {
		return
	}
	n += 1

	if int(l) < MinEntropyBytes || int(l) > MaxEntropyBytes || int(l)%EntropyStepBytes != 0 {
		return 0, ErrInvalidEntropyLength
	}

	buf := make([]byte, l)
	var m int
	m, err = io.ReadFull(r, buf)
	n += int64(m)
	if err != nil {
		return
	}

	*e = buf
	return
}

func (e Entropy) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(e)), nil
}

func (e *Entropy) UnmarshalText(text []byte) error {
	decoded, err := hex.DecodeString(string(text))
	if err != nil {
		return err
	}
	*e = decoded
	return nil
}

func init() {
	_ = astral.Add(&Entropy{})
}
