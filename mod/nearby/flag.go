package nearby

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type Flag string

func NewFlag(f string) *Flag { return (*Flag)(&f) }

func (f Flag) String() string { return string(f) }

// astral:blueprint-ignore
func (Flag) ObjectType() string { return "mod.nearby.flag" }

func (f Flag) WriteTo(w io.Writer) (n int64, err error) {
	return astral.String8(f).WriteTo(w)
}

func (f *Flag) ReadFrom(r io.Reader) (n int64, err error) {
	return (*astral.String8)(f).ReadFrom(r)
}

// json

func (f *Flag) UnmarshalJSON(bytes []byte) error {
	return json.Unmarshal(bytes, (*string)(f))
}

func (f Flag) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}

// text

func (f *Flag) UnmarshalText(text []byte) error {
	*f = Flag(text)
	return nil
}

func (f Flag) MarshalText() (text []byte, err error) {
	return []byte(f), nil
}

// ...

func init() {
	_ = astral.Add(NewFlag(""))
}
