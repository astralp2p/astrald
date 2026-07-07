package nat

import "github.com/cryptopunkscc/astral-go/astral"

const ModuleName = "nat"

// Module defines the NAT traversal module public API.
type Module interface{}

const (
	MethodPunch           = "nat.punch"
	MethodListHoles       = "nat.list_holes"
	MethodSetEnabled      = "nat.set_enabled"
	MethodNodePunch       = "nat.node_punch"
	MethodNodeConsumeHole = "nat.node_consume_hole"
)

func init() {
	_ = astral.Add(&PunchSignal{})
	_ = astral.Add(&ConsumeHoleSignal{})
}
