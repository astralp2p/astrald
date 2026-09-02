package mcp

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// A caller reads its outbox row differently depending on whether the message is
// known not to be stored or merely not known to be, so delivery names the three
// outcomes apart.
var (
	errNotSent  = errors.New("the message did not leave this node")
	errRefused  = errors.New("the recipient's node refused it")
	errNoAnswer = errors.New("the message left and nothing came back")
)

// sendMessage puts one message to a recipient and records what became of it.
// It answers the id the message was stored under, which is also the value a
// later message names as its parent.
func (mod *Module) sendMessage(agentID *astral.Identity, to, content string, parent mcp.MessageID) (id mcp.MessageID, err error) {
	targetID, err := mod.resolveRecipient(agentID, to)
	if err != nil {
		return id, err
	}

	if len(content) > mod.config.MaxPayloadBytes {
		return id, fmt.Errorf("content is over %v bytes", mod.config.MaxPayloadBytes)
	}

	// why the parent is checked here too: the recipient's node refuses a parent
	// it does not hold, so a send naming one this agent cannot see would write
	// an outbox row and then fail delivery.
	if !parent.IsZero() {
		held, err := mod.db.Holds(agentID, parent)
		if err != nil {
			return id, err
		}
		if !held {
			return id, errors.New("cannot answer a message you do not hold")
		}
	}

	msg := &mcp.Message{
		ID:       mcp.NewMessageID(),
		Content:  astral.String32(content),
		ParentID: parent,
	}

	// why nothing above this writes a row: a stored list of refusals would tell
	// a recipient that refuses apart from one that does not exist, which is the
	// collapse resolveRecipient is built on.
	if err = mod.db.InsertOutbox(&dbMessage{
		ID:        msg.ID,
		Sender:    agentID,
		Recipient: targetID,
		Content:   content,
		ParentID:  parent,
	}); err != nil {
		return id, err
	}

	if err = mod.deliverMessage(agentID, targetID, msg); err != nil {
		mod.noteDeliveryFailed(agentID, msg.ID, err)

		// why the caller is told the same words in all three cases: which one
		// happened is a fact about the row, and the sent list is what answers it.
		return id, fmt.Errorf("delivery failed: %v", err)
	}

	if err = mod.db.StampLanded(agentID, msg.ID); err != nil {
		mod.log.Error("outbox %v: stamping landed_at: %v", msg.ID, err)
	}

	return msg.ID, nil
}

// resolveRecipient answers who a name means, if this agent may reach them.
//
// why all three refusals answer the same words: an agent learns that it cannot
// reach this recipient, and not whether the recipient exists.
func (mod *Module) resolveRecipient(agentID *astral.Identity, to string) (*astral.Identity, error) {
	unknown := fmt.Errorf("unknown recipient: %v", to)

	targetID, err := mod.Dir.ResolveIdentity(to)
	if err != nil {
		return nil, unknown
	}

	// why the empty name is refused here: ResolveIdentity answers the Anyone
	// identity for an empty string rather than an error, and Anyone is a target
	// the authorizer and the router would both accept.
	if targetID.IsZero() {
		return nil, unknown
	}

	// The same question astral-query asks about the same pair: what this agent
	// may reach is its owner's decision, and a message buys it no reach it did
	// not have.
	if !mod.Auth.Authorize(mod.ctx, &mcp.CallAgentAction{
		Action: auth.NewAction(agentID),
		ToID:   targetID,
	}) {
		return nil, unknown
	}

	return targetID, nil
}

// noteDeliveryFailed records what became of a send that did not land.
//
// why errNoAnswer stamps nothing: an answer that never arrived proves nothing
// about the write, and the row that says nothing is the row that is right.
//
// why a failed stamp is logged and not returned: the delivery happened as it
// happened, and every state here is read off which instants are set.
func (mod *Module) noteDeliveryFailed(agentID *astral.Identity, id mcp.MessageID, cause error) {
	if !errors.Is(cause, errRefused) && !errors.Is(cause, errNotSent) {
		return
	}

	if err := mod.db.StampFailed(agentID, id); err != nil {
		mod.log.Error("outbox %v: stamping failed_at: %v", id, err)
	}

	if !errors.Is(cause, errRefused) {
		return
	}
	if err := mod.db.SetErr(agentID, id, cause.Error()); err != nil {
		mod.log.Error("outbox %v: recording the refusal: %v", id, err)
	}
}
