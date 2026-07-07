package crypto

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
)

type PublicKey struct {
	Type astral.String8
	Key  astral.Bytes16
}

var _ astral.Object = &PublicKey{}

func (PublicKey) ObjectType() string {
	return "mod.crypto.public_key"
}

// binary

func (s PublicKey) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *PublicKey) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(s).ReadFrom(r)
}

// json

func (s PublicKey) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&s).MarshalJSON()
}

func (s *PublicKey) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(s).UnmarshalJSON(bytes)
}

// text

func (s PublicKey) MarshalText() (text []byte, err error) {
	return []byte(fmt.Sprintf("%s:%s", s.Type, hex.EncodeToString(s.Key))), nil
}

func (s *PublicKey) UnmarshalText(text []byte) (err error) {
	parts := strings.SplitN(string(text), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format")
	}

	s.Key, err = hex.DecodeString(parts[1])
	if err != nil {
		return err
	}

	s.Type = astral.String8(parts[0])

	return
}

// ...

func init() {
	_ = astral.Add(&PublicKey{})
}
