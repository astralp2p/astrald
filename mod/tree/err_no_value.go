package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

// ErrNoValue is returned when a node holds no value; it also implements astral.Object so it can be transmitted over the wire.
type ErrNoValue struct {
	astral.EmptyObject
}

var _ astral.Object = &ErrNoValue{}

func (err ErrNoValue) ObjectType() string {
	return "mod.tree.err_no_value"
}

func (err *ErrNoValue) Error() string {
	return "no value is set"
}

func (err *ErrNoValue) Is(other error) bool {
	_, ok := other.(*ErrNoValue)
	return ok
}

func init() {
	_ = astral.Add(&ErrNoValue{})
}
