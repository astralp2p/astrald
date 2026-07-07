package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// Probe contains the result of probing an object.
type Probe struct {
	Type astral.String8  // Type of the object, if any
	Repo astral.String8  // Repo the object was found in
	Mime astral.String8  // Mime type of the object
	Time astral.Duration // Time it took to probe the object
}

var _ astral.Object = &Probe{}

func (Probe) ObjectType() string {
	return "mod.objects.probe"
}

// binary

func (s Probe) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *Probe) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(s).ReadFrom(r)
}

// json

func (s Probe) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&s).MarshalJSON()
}

func (s *Probe) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(s).UnmarshalJSON(bytes)
}

// ...

func init() {
	_ = astral.Add(&Probe{})
}
