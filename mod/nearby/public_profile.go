package nearby

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type PublicProfile struct {
	NodeID    *astral.Identity
	NodeAlias string
}

var _ astral.Object = &PublicProfile{}

func (PublicProfile) ObjectType() string {
	return "mod.nearby.public_profile"
}

// binary

func (s PublicProfile) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *PublicProfile) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(s).ReadFrom(r)
}

// json

func (s PublicProfile) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&s).MarshalJSON()
}

func (s *PublicProfile) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(s).UnmarshalJSON(bytes)
}

// ...

func init() {
	_ = astral.Add(&PublicProfile{})
}
