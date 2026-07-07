package crypto

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
)

type PrivateKey struct {
	Type astral.String8
	Key  astral.Bytes16
}

var _ astral.Object = &PrivateKey{}

func (PrivateKey) ObjectType() string {
	return "mod.crypto.private_key"
}

// binary

func (key PrivateKey) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&key).WriteTo(w)
}

func (key *PrivateKey) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(key).ReadFrom(r)
}

// json

func (key PrivateKey) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&key).MarshalJSON()
}

func (key *PrivateKey) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(key).UnmarshalJSON(bytes)
}

// text

func (key PrivateKey) MarshalText() (text []byte, err error) {
	s := fmt.Sprintf("%s:%s", key.Type, base64.StdEncoding.EncodeToString(key.Key))
	return []byte(s), nil
}

func (key *PrivateKey) UnmarshalText(text []byte) error {
	parts := strings.SplitN(string(text), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format")
	}

	bytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}

	key.Type = astral.String8(parts[0])
	key.Key = bytes

	return nil
}

// ...

func (key PrivateKey) String() string {
	return key.Type.String() + ":" + base64.StdEncoding.EncodeToString(key.Key)
}

func init() {
	astral.Add(&PrivateKey{})
}
