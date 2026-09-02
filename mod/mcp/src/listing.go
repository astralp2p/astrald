package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// errUnknownPeer is the one answer a name that does not resolve gets: a caller
// cannot tell a correspondent it may not reach from one that does not exist.
func errUnknownPeer(name string) error {
	return &unknownPeerError{name}
}

type unknownPeerError struct{ name string }

func (e *unknownPeerError) Error() string { return "unknown correspondent: " + e.name }

// listRequest is one of the three lists, in the words an agent asked for it.
type listRequest struct {
	List           string
	From, To       string
	Since          string
	UnreadOnly     bool
	AwaitingPickup bool
}

// query turns the agent's words into the store's: a name becomes an identity in
// exactly one place, and a filter that cannot apply is refused here.
func (mod *Module) query(req listRequest) (q messageQuery, err error) {
	q = messageQuery{
		List:           req.List,
		UnreadOnly:     req.UnreadOnly,
		AwaitingPickup: req.AwaitingPickup,
	}

	if req.Since != "" {
		if q.Since, err = parseSince(req.Since); err != nil {
			return q, err
		}
	}
	if req.From != "" {
		if q.From, err = mod.Dir.ResolveIdentity(req.From); err != nil {
			return q, errUnknownPeer(req.From)
		}
	}
	if req.To != "" {
		if q.To, err = mod.Dir.ResolveIdentity(req.To); err != nil {
			return q, errUnknownPeer(req.To)
		}
	}

	return q, q.validate()
}

// listMessages answers one of the agent's three lists.
func (mod *Module) listMessages(agentID *astral.Identity, req listRequest) ([]*mcp.StoredMessage, error) {
	q, err := mod.query(req)
	if err != nil {
		return nil, err
	}
	return mod.db.ListMessages(agentID, q)
}
