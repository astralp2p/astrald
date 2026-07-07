package dir

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// AliasMap maps aliases to identities. Aliases is constructed in one shot by the producer
// (mod/dir.AliasMap) and is read-only after return; no concurrent writers.
type AliasMap struct {
	Aliases map[string]*astral.Identity
}

func (a AliasMap) ObjectType() string {
	return "mod.dir.alias_map"
}

func (a AliasMap) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *AliasMap) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a AliasMap) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&a).MarshalJSON()
}

func (a *AliasMap) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(a).UnmarshalJSON(bytes)
}

func init() {
	astral.Add(&AliasMap{})
}
