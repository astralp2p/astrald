package messaging

import (
	"errors"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

var _ messagingmod.Module = &Module{}

// errNotParticipant is what a mail method answers for an identity whose mailbox
// this node does not host.
var errNotParticipant = errors.New("not a messaging participant")

// errAnotherMailbox is what a mail method answers for a request that names a
// mailbox other than its owner's. A method acts on the mailbox it is handed; a
// delegated read is an operation's, which asks auth about its caller.
var errAnotherMailbox = errors.New("a mail method reads its owner's own mailbox")

type Module struct {
	Deps
	ctx    *astral.Context
	config Config
	node   astral.Node
	log    *log.Logger
	db     *DB
	router routing.OpRouter

	// mailboxes is the hosting index, keyed by identity. It mirrors
	// messaging__mailboxes and authorizes nothing by itself — see hosts.
	mailboxes sig.Map[string, mailbox]

	// mu orders each write to messaging__mailboxes with its mirror in
	// mailboxes, so a creation and a deletion cannot leave the mirror naming a
	// row the table no longer holds. A mail row is inserted under the read
	// lock — see whileIndexed.
	mu sync.RWMutex

	// waiters are the parked waits, woken when a row enters their set.
	waiters waiters
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

// RoutingPriority puts the mailbox ahead of the node-level routers.
//
// why: a delivery is addressed to a mailbox this node hosts, and mod/nodes at
// the normal priority would otherwise try to reach that identity over a link
// first.
func (mod *Module) RoutingPriority() int {
	return astral.RoutingPriorityMedium
}

func (mod *Module) String() string {
	return messagingmod.ModuleName
}
