package routing

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
)

type OpSpec struct {
	Name       string
	Parameters []query.FieldSpec
}

var _ astral.Object = &OpSpec{}

func (OpSpec) ObjectType() string {
	return "routing.op_spec"
}

// binary

func (s OpSpec) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *OpSpec) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(s).ReadFrom(r)
}

// json

func (s OpSpec) MarshalJSON() ([]byte, error) {
	return astral.Objectify(&s).MarshalJSON()
}

func (s *OpSpec) UnmarshalJSON(bytes []byte) error {
	return astral.Objectify(s).UnmarshalJSON(bytes)
}

// ...

func init() {
	_ = astral.Add(&OpSpec{})
}
