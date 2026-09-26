package messaging

import (
	"context"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// errUnknownPeer is the one answer a name that does not resolve gets: a caller
// cannot tell a correspondent it may not reach from one that does not exist.
func errUnknownPeer(name string) error {
	return &unknownPeerError{name}
}

type unknownPeerError struct{ name string }

func (e *unknownPeerError) Error() string { return "unknown correspondent: " + e.name }

// ListMessages answers one of the owner's three lists, without bodies. A
// request whose Mailbox names another identity is refused: the method lists the
// owner's own mailbox, and a delegated read is messaging.list_messages', which
// asks auth about its caller.
func (mod *Module) ListMessages(_ context.Context, owner *astral.Identity, req messaging.ListMessagesRequest) ([]*messaging.Envelope, error) {
	if !mod.namesOwnMailbox(owner, req.Mailbox) {
		return nil, errAnotherMailbox
	}

	return mod.listMailbox(owner, req)
}

// listMailbox answers one of the three lists of a mailbox this node hosts,
// without bodies. A listing hands no body out, so it stamps nothing, whoever
// asked for it.
func (mod *Module) listMailbox(mailbox *astral.Identity, req messaging.ListMessagesRequest) ([]*messaging.Envelope, error) {
	if !mod.hosts(mailbox) {
		return nil, errNotParticipant
	}

	rows, err := mod.listMessages(mailbox, req)
	if err != nil {
		return nil, err
	}

	return envelopes(rows), nil
}

// namesOwnMailbox answers whether a mailbox name leaves the owner's own
// mailbox: the empty name, or one that resolves to the owner.
func (mod *Module) namesOwnMailbox(owner *astral.Identity, name string) bool {
	if name == "" {
		return true
	}

	mailbox, err := mod.Dir.ResolveIdentity(name)
	return err == nil && mailbox.IsEqual(owner)
}

// query turns the participant's words into the store's: a name becomes an
// identity in exactly one place, and a filter that cannot apply is refused here.
func (mod *Module) query(req messaging.ListMessagesRequest) (q messageQuery, err error) {
	q = messageQuery{
		List:           req.List,
		UnreadOnly:     req.UnreadOnly,
		AwaitingPickup: req.AwaitingPickup,
	}

	if q.Since, err = sinceOf(req.Since); err != nil {
		return q, err
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

// listMessages answers one of the owner's three lists.
func (mod *Module) listMessages(owner *astral.Identity, req messaging.ListMessagesRequest) ([]*messaging.StoredMessage, error) {
	q, err := mod.query(req)
	if err != nil {
		return nil, err
	}
	return mod.db.ListMessages(owner, q)
}

// envelopes renders a listing without its bodies, for a listing and a wait
// alike.
func envelopes(rows []*messaging.StoredMessage) []*messaging.Envelope {
	list := make([]*messaging.Envelope, len(rows))
	for i, row := range rows {
		list[i] = row.Envelope()
	}
	return list
}
