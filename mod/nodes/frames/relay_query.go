package frames

import (
	"fmt"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ Frame = &RelayQuery{}

type RelayQuery struct {
	CallerID *astral.Identity
	TargetID *astral.Identity
	Query    Query
}

// astral:blueprint-ignore
func (frame *RelayQuery) ObjectType() string {
	return "nodes.frames.relay_query"
}

func (frame *RelayQuery) ReadFrom(r io.Reader) (n int64, err error) {
	frame.CallerID = new(astral.Identity)
	m, err := frame.CallerID.ReadFrom(r)
	n += m
	if err != nil {
		return
	}

	frame.TargetID = new(astral.Identity)
	m, err = frame.TargetID.ReadFrom(r)
	n += m
	if err != nil {
		return
	}

	m, err = frame.Query.ReadFrom(r)
	n += m
	return
}

func (frame *RelayQuery) WriteTo(w io.Writer) (n int64, err error) {
	// why: astral.Identity declares WriteTo on the value receiver, so a nil pointer panics instead of erroring.
	// why: a nil identity encodes as the zero identity, the 33 null bytes ReadFrom decodes to a zero identity.
	callerID, targetID := frame.CallerID, frame.TargetID
	if callerID == nil {
		callerID = &astral.Identity{}
	}
	if targetID == nil {
		targetID = &astral.Identity{}
	}

	m, err := callerID.WriteTo(w)
	n += m
	if err != nil {
		return
	}

	m, err = targetID.WriteTo(w)
	n += m
	if err != nil {
		return
	}

	m, err = frame.Query.WriteTo(w)
	n += m
	return
}

func (frame *RelayQuery) String() string {
	return fmt.Sprintf("relay_query(%s -> %s: '%s')", frame.CallerID, frame.TargetID, frame.Query.Query)
}
