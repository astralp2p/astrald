package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "messaging"
const DBPrefix = "messaging__"

// Module is the public API surface of the messaging module.
//
// The types the module works in are declared by astral-go's api/messaging and
// registered there. The store's own row types stay in src and never leave it.
//
// Every mail method acts on the boxes of the identity it is handed, whose
// mailbox this node must host: the hosting index names it and auth finds this
// node holds mod.messaging.host_mailbox_action for it under a contract the
// identity signed. A request whose Mailbox names another identity is refused
// with an error: a delegated read belongs to the messaging.list_messages and
// messaging.read_messages operations, which ask auth about their caller. The
// node identity is never a participant.
type Module interface {
	CreateIdentity(ctx *astral.Context, alias string, duration astral.Duration) (*messaging.IdentityCredential, error)
	FindIdentity(identity *astral.Identity) error
	DeleteIdentity(ctx *astral.Context, identity *astral.Identity) error
	SendMessage(ctx context.Context, sender *astral.Identity, req *messaging.SendMessageRequest) (messaging.MessageID, error)
	ListMessages(ctx context.Context, owner *astral.Identity, req messaging.ListMessagesRequest) ([]*messaging.Envelope, error)
	ReadMessages(ctx context.Context, owner *astral.Identity, req *messaging.ReadMessagesRequest) (*messaging.ReadMessagesResult, error)
	Wait(ctx context.Context, owner *astral.Identity, req messaging.WaitRequest, report ProgressFunc) (*messaging.WaitResult, error)
	Archive(ctx context.Context, owner *astral.Identity, ref messaging.MessageRef, undo bool) (bool, error)
}

// ProgressFunc announces a park still held, given what it has spent of its
// window and what it was granted. A nil ProgressFunc is a caller that asked for
// no reports.
type ProgressFunc func(spent, granted time.Duration)

// ErrIdentityNotFound is what FindIdentity and DeleteIdentity answer for an
// identity whose mailbox the hosting index does not name.
var ErrIdentityNotFound = errors.New("identity not found")
