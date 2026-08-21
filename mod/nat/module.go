package nat

import "github.com/astralp2p/astral-go/astral"
import (
	"github.com/astralp2p/astral-go/api/nat"
)

const ModuleName = "nat"

// Module defines the NAT traversal module public API.
type Module interface{}

func init() {
	_ = astral.Add(&nat.PunchSignal{})
	_ = astral.Add(&nat.ConsumeHoleSignal{})
}
