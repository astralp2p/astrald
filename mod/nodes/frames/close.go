package frames

import (
	"fmt"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ Frame = &Close{}

// CloseReason names why the sender of a Close frame closes the link.
// note: the set is append-only (astral-docs topics/link-mux.md, close.Reason);
// a later value is added at the end, and no value changes or is reused.
type CloseReason uint8

const (
	// CloseClosed is a close for a cause no other value names.
	CloseClosed CloseReason = 0
	// CloseExcess is a close of a link the sender holds another link beside.
	CloseExcess CloseReason = 1
	// CloseShutdown is a close by a node that is shutting down.
	CloseShutdown CloseReason = 2
)

// Known returns r, or CloseClosed when r is a value this node does not know.
func (r CloseReason) Known() CloseReason {
	if r > CloseShutdown {
		return CloseClosed
	}
	return r
}

func (r CloseReason) String() string {
	switch r {
	case CloseClosed:
		return "closed"
	case CloseExcess:
		return "excess"
	case CloseShutdown:
		return "shutdown"
	default:
		return fmt.Sprintf("reason %d", uint8(r))
	}
}

// Close announces that the sender closes the link the frame arrives on.
// The receiver closes its end and sends nothing back.
type Close struct {
	Reason CloseReason
}

// astral:blueprint-ignore
func (frame *Close) ObjectType() string {
	return "nodes.frames.close"
}

func (frame *Close) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(frame).ReadFrom(r)
}

func (frame *Close) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(frame).WriteTo(w)
}

func (frame *Close) String() string {
	return fmt.Sprintf("close(%s)", frame.Reason)
}
