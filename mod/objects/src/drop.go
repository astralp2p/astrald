package objects

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type Drop struct {
	mod      *Module
	senderID *astral.Identity
	object   astral.Object
	repo     objects.Repository
	accepted sig.Value[bool]
	saved    bool
	mu       sync.Mutex
}

func (drop *Drop) SenderID() *astral.Identity {
	return drop.senderID
}

func (drop *Drop) Object() astral.Object {
	return drop.object
}

// Accept marks the drop accepted and, if save is true, persists the object to
// the target repo. Persistence is idempotent: a second save call is a no-op.
func (drop *Drop) Accept(save bool) error {
	drop.accepted.Set(true)
	if !save {
		return nil
	}

	drop.mu.Lock()
	defer drop.mu.Unlock()

	if drop.saved {
		return nil
	}

	ctx := astral.NewContext(nil)

	_, err := drop.mod.Store(ctx, drop.repo, drop.object)
	if err != nil {
		drop.mod.log.Error("error saving received object: %v", err)
	} else {
		drop.saved = true
	}

	return err
}
