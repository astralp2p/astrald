package dir

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Alias holds a human-readable name for a node. Defined in mod/dir as it is
// a directory concept; may be used by any module that broadcasts presence information.
type Alias string

func (Alias) ObjectType() string { return "mod.dir.alias" }

func (a Alias) WriteTo(w io.Writer) (int64, error)   { return astral.String8(a).WriteTo(w) }
func (a *Alias) ReadFrom(r io.Reader) (int64, error) { return (*astral.String8)(a).ReadFrom(r) }

func (a Alias) MarshalJSON() ([]byte, error)     { return json.Marshal(string(a)) }
func (a *Alias) UnmarshalJSON(b []byte) error    { return json.Unmarshal(b, (*string)(a)) }
func (a Alias) MarshalText() ([]byte, error)     { return []byte(a), nil }
func (a *Alias) UnmarshalText(text []byte) error { *a = Alias(text); return nil }

func (a Alias) String() string { return string(a) }

func init() {
	var a Alias
	_ = astral.Add(&a)
}
