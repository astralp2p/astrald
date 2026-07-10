package frames

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
)

// Frame is a message exchanged over a node link.
type Frame interface {
	astral.Object
	fmt.Stringer
}

// FrameTypes maps frame opcodes to object types by slice index.
// note: order is the wire opcode; never reorder or remove entries.
var FrameTypes = []string{
	"nodes.frames.ping",
	"nodes.frames.query",
	"nodes.frames.relay_query",
	"nodes.frames.read",
	"nodes.frames.response",
	"nodes.frames.data",
	"nodes.frames.migrate",
	"nodes.frames.reset",
}

var FrameTypeEncoder = astral.IndexedTypeEncoder(FrameTypes)
var FrameTypeDecoder = astral.IndexedTypeDecoder(FrameTypes)

func init() {
	_ = astral.Add(
		&Ping{},
		&Query{},
		&RelayQuery{},
		&Response{},
		&Read{},
		&Data{},
		&Migrate{},
		&Reset{},
	)
}
