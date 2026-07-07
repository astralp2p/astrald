package frames

import (
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
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
	m, err := frame.CallerID.WriteTo(w)
	n += m
	if err != nil {
		return
	}

	m, err = frame.TargetID.WriteTo(w)
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
