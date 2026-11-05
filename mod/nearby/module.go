package nearby

import (
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

const (
	ModuleName = "nearby"
)

// Module is the public API of the nearby module: it sends the node's status and scans
// on the local network, and holds the node's nearby mode.
type Module interface {
	// Broadcast sends the node's status on every local subnet. It sends nothing in
	// ModeSilent, or when the composers attach nothing.
	Broadcast() error
	// Scan asks the nodes on every local subnet for their status. It sends nothing in
	// ModeSilent.
	Scan() error
	AddStatusComposer(Composer)
	Mode() nearby.Mode
	// SetMode stores the mode. It refuses a mode above nearby.ModeStealth with
	// ErrInvalidMode.
	SetMode(ctx *astral.Context, m nearby.Mode) error
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

// Recognizer reports who sent a status (stealth hint or public profile), if it can tell.
type Recognizer interface {
	// ok == true:  nodeID is set (never nil); userID is set if known, else nil.
	// ok == false: this recognizer can't tell who sent this status; nothing else is implied.
	RecognizeStatus(status *nearby.StatusMessage) (nodeID, userID *astral.Identity, ok bool)
}
