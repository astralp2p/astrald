package nearby

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/nearby"
)

const MaxAttachmentSize = 4 * 1024 // 4 KiB

var _ nearby.Composition = &Composition{}

// Composition is the mutable context passed to each Composer during status construction,
// scoping attachments to a specific receiver and enforcing the size limit.
type Composition struct {
	receiver *astral.Identity
	s        *nearby.StatusMessage
}

func (c *Composition) Receiver() *astral.Identity {
	return c.receiver
}

// Attach appends an object to the status message, rejecting it if its serialized
// size exceeds MaxAttachmentSize.
func (c *Composition) Attach(o astral.Object) error {
	objectID, _ := astral.ResolveObjectID(o)
	if objectID.Size > MaxAttachmentSize {
		return nearby.ErrObjectTooLarge
	}
	return c.s.Attachments.Append(o)
}
