package bip137sig

import (
	"encoding/binary"
	"encoding/hex"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &Seed{}

type Seed []byte

func (Seed) ObjectType() string {
	return "bip137sig.seed"
}

func (s Seed) WriteTo(w io.Writer) (n int64, err error) {
	l := uint8(len(s))

	if len(s) != SeedLengthBytes {
		return 0, ErrInvalidSeedLength
	}

	if err = binary.Write(w, astral.ByteOrder, &l); err != nil {
		return
	}
	n += 1

	var m int
	m, err = w.Write(s)
	n += int64(m)
	return
}

func (s *Seed) ReadFrom(r io.Reader) (n int64, err error) {
	var l uint8
	if err = binary.Read(r, astral.ByteOrder, &l); err != nil {
		return
	}
	n += 1

	if int(l) != SeedLengthBytes {
		return n, ErrInvalidSeedLength
	}

	buf := make([]byte, l)
	var m int
	m, err = io.ReadFull(r, buf)
	n += int64(m)
	if err != nil {
		return
	}

	*s = buf
	return
}

func (s Seed) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(s)), nil
}

func (s *Seed) UnmarshalText(text []byte) error {
	decoded, err := hex.DecodeString(string(text))
	if err != nil {
		return err
	}
	*s = decoded
	return nil
}

func init() {
	_ = astral.Add(&Seed{})
}
