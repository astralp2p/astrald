package nearby

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// Mode describes how a node broadcasts its presence in the local network.
type Mode astral.Uint8

const (
	ModeSilent  Mode = iota // do not broadcast
	ModeVisible             // broadcast; content depends on contract state
	ModeStealth             // broadcast with masked identity (hides user-node association)
)

func (*Mode) ObjectType() string { return "mod.nearby.mode" }

// UnderlyingPrimitive satisfies astral.PrimitiveAlias so blueprint sync derives an
// alias-kind *astral.Blueprint for Mode and peers without the nearby module
// decode the byte as *astral.RuntimeObject of "mod.nearby.mode".
func (*Mode) UnderlyingPrimitive() string { return "uint8" }

func (m *Mode) WriteTo(w io.Writer) (int64, error) {
	return (*astral.Uint8)(m).WriteTo(w)
}

func (m *Mode) ReadFrom(r io.Reader) (int64, error) {
	return (*astral.Uint8)(m).ReadFrom(r)
}

func init() {
	var mode Mode
	_ = astral.Add(&mode)
}
