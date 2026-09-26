package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A caller reads its outbox row differently depending on whether the message is
// known not to be stored or merely not known to be, so delivery names the four
// outcomes apart.
//
// why errNotAdmitted is apart from errUnreachable: a sender that is turned away
// must stop and ask its operator, and a sender that found nobody must retry
// later. One answer for both left every failure looking like the other.
//
// why errUnreachable still names two causes: an identity no node holds and a
// node that could not be reached both answer with one silence, and no code
// tells them apart.
var (
	errNotAdmitted = errors.New("the recipient does not take messages from you")
	errUnreachable = errors.New("the recipient took nothing; they may not exist, or their node may be unreachable")
	errNotSent     = errors.New("the message did not leave this node")
	errRefused     = errors.New("the recipient's node refused it")
	errNoAnswer    = errors.New("the message left and nothing came back")
)

// SendMessage puts one message from the sender to the recipient the request
// names, and answers the id it is stored under.
func (mod *Module) SendMessage(_ context.Context, sender *astral.Identity, req *messaging.SendMessageRequest) (id messaging.MessageID, err error) {
	if !mod.hosts(sender) {
		return id, errNotParticipant
	}
	if req == nil {
		return id, errors.New("the request names no message")
	}

	return mod.sendMessage(sender, string(req.To), string(req.Content), req.ParentID)
}

// sendMessage puts one message to a recipient and records what became of it.
// It answers the id the message was stored under, which is also the value a
// later message names as its parent.
func (mod *Module) sendMessage(senderID *astral.Identity, to, content string, parent messaging.MessageID) (id messaging.MessageID, err error) {
	targetID, err := mod.resolveRecipient(senderID, to)
	if err != nil {
		return id, err
	}

	if len(content) > mod.config.MaxPayloadBytes {
		return id, fmt.Errorf("content is over %v bytes", mod.config.MaxPayloadBytes)
	}

	if err = mod.checkParent(senderID, parent); err != nil {
		return id, err
	}

	msg := &messaging.Message{
		ID:       messaging.NewMessageID(),
		Content:  astral.String32(content),
		ParentID: parent,
	}

	// why nothing above this writes a row: a stored list of refusals would tell
	// a recipient that refuses apart from one that does not exist, which is the
	// collapse resolveRecipient is built on.
	err = mod.whileIndexed(senderID, func() error {
		return mod.db.InsertOutbox(&messaging.StoredMessage{
			ID:        msg.ID,
			Sender:    senderID,
			Recipient: targetID,
			Content:   msg.Content,
			ParentID:  parent,
		})
	})
	if err != nil {
		return id, err
	}

	if err = mod.deliverMessage(senderID, targetID, msg); err != nil {
		mod.noteDeliveryFailed(senderID, msg.ID, err)

		// why the caller is told the same words in all three cases: which one
		// happened is a fact about the row, and the sent list is what answers it.
		return id, fmt.Errorf("delivery failed: %v", err)
	}

	if err = mod.db.StampLanded(senderID, msg.ID); err != nil {
		mod.log.Error("outbox %v: stamping landed_at: %v", msg.ID, err)
	}

	return msg.ID, nil
}

// checkParent refuses a parent the sender does not hold.
//
// why the parent is checked here too: the recipient's node refuses a parent it
// does not hold, so a send naming one this participant cannot see would write
// an outbox row and then fail delivery.
func (mod *Module) checkParent(senderID *astral.Identity, parent messaging.MessageID) error {
	if parent.IsZero() {
		return nil
	}

	held, err := mod.db.Holds(senderID, parent)
	if err != nil {
		return err
	}
	if !held {
		return errors.New("cannot answer a message you do not hold")
	}
	return nil
}

// resolveRecipient answers who a name means, if this participant may reach
// them.
//
// why all three refusals answer the same words: a participant learns that it
// cannot reach this recipient, and not whether the recipient exists.
func (mod *Module) resolveRecipient(senderID *astral.Identity, to string) (*astral.Identity, error) {
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

	// why the sender's own side is asked: whom this participant may write to is
	// its owner's decision, and whether the recipient takes it is asked
	// separately where the delivery arrives.
	if !mod.Auth.Authorize(mod.ctx, &messaging.SendAction{
		Action: auth.NewAction(senderID),
		ToID:   targetID,
	}) {
		return nil, unknown
	}

	return targetID, nil
}

// noteDeliveryFailed records what became of a send that did not land.
//
// why errNoAnswer is the only outcome that stamps nothing: an answer that never
// arrived proves nothing about the write, and the row that says nothing is the
// row that is right. Every other outcome is a delivery known not to be stored.
//
// why a failed stamp is logged and not returned: the delivery happened as it
// happened, and every state here is read off which instants are set.
func (mod *Module) noteDeliveryFailed(senderID *astral.Identity, id messaging.MessageID, cause error) {
	if errors.Is(cause, errNoAnswer) {
		return
	}

	if err := mod.db.StampFailed(senderID, id); err != nil {
		mod.log.Error("outbox %v: stamping failed_at: %v", id, err)
	}

	// why only these two leave words: they are the recipient's side saying no,
	// and the row is where the sender reads that back after the call returned.
	if !errors.Is(cause, errRefused) && !errors.Is(cause, errNotAdmitted) {
		return
	}
	if err := mod.db.SetErr(senderID, id, cause.Error()); err != nil {
		mod.log.Error("outbox %v: recording the refusal: %v", id, err)
	}
}
