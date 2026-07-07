package crypto

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Signature is a generic container for a signature
type Signature struct {
	Scheme astral.String8
	Data   astral.Bytes16
}

var _ astral.Object = &Signature{}

func (Signature) ObjectType() string {
	return "mod.crypto.signature"
}

// binary

func (sig Signature) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&sig).WriteTo(w)
}

func (sig *Signature) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(sig).ReadFrom(r)
}

// json

func (sig Signature) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&sig).MarshalJSON()
}

func (sig *Signature) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(sig).UnmarshalJSON(bytes)
}

// text

func (sig Signature) MarshalText() (text []byte, err error) {
	str := fmt.Sprintf("%s:%s", sig.Scheme, base64.StdEncoding.EncodeToString(sig.Data))
	return []byte(str), nil
}

func (sig *Signature) UnmarshalText(text []byte) error {
	parts := strings.SplitN(string(text), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format")
	}

	bytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}

	sig.Scheme = astral.String8(parts[0])
	sig.Data = bytes

	return nil
}

// ...

func init() {
	_ = astral.Add(&Signature{})
}
