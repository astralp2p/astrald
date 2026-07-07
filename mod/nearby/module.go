package nearby

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

const (
	ModuleName = "nearby"
)

// Module is the public API of the nearby module; implementations manage broadcast state,
// status composition, and stealth-mode identity resolution.
type Module interface {
	Broadcast() error
	AddStatusComposer(Composer)
	Mode() Mode
	SetMode(ctx *astral.Context, m Mode) error
	ResolveStatus(status *StatusMessage) *astral.Identity
}

// Composer contributes attachments to an outgoing status broadcast.
type Composer interface {
	ComposeStatus(Composition)
}

// Composition is the mutable context passed to each Composer during a broadcast cycle.
type Composition interface {
	Receiver() *astral.Identity
	Attach(astral.Object) error
}
