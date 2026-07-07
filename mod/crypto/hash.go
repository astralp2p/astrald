package crypto

import (
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type Hash []byte

var _ astral.Object = &Hash{}

func (Hash) ObjectType() string {
	return "mod.crypto.hash"
}

// binary

func (hash Hash) WriteTo(w io.Writer) (int64, error) {
	return astral.Bytes8(hash).WriteTo(w)
}

func (hash *Hash) ReadFrom(r io.Reader) (int64, error) {
	return (*astral.Bytes8)(hash).ReadFrom(r)
}

// json

func (hash Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(hash))
}

func (hash *Hash) UnmarshalJSON(bytes []byte) (err error) {
	var hashHex string
	err = json.Unmarshal(bytes, &hashHex)
	if err != nil {
		return
	}
	*hash, err = hex.DecodeString(hashHex)

	return
}

// text

func (hash Hash) MarshalText() (text []byte, err error) {
	return []byte(hex.EncodeToString(hash)), nil
}

func (hash *Hash) UnmarshalText(text []byte) (err error) {
	*hash, err = hex.DecodeString(string(text))
	return
}

// ...

func init() {
	_ = astral.Add(&Hash{})
}
