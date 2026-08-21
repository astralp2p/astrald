package nat

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nat"
)

// DiscoverServices sends the current enabled state immediately, then streams updates on every enabled change.
// When follow is false the channel is closed after the initial snapshot.
func (mod *Module) DiscoverServices(
	ctx *astral.Context,
	caller *astral.Identity,
	follow bool,
) (<-chan *services.Update, error) {
	var ch = make(chan *services.Update, 2)

	if mod.enabled.Load() {
		ch <- mod.newServiceUpdate(true)
	}

	if !follow {
		close(ch)
		return ch, nil
	}

	ch <- nil

	go func() {
		<-ctx.Done()
		mod.cond.Broadcast()
	}()

	go func() {
		mod.cond.L.Lock()
		defer mod.cond.L.Unlock()
		for {
			mod.cond.Wait()
			select {
			case <-ctx.Done():
				return
			case ch <- mod.newServiceUpdate(mod.enabled.Load()):
			}

		}
	}()

	return ch, nil
}

func (mod *Module) newServiceUpdate(available bool) *services.Update {
	return &services.Update{
		Available:  available,
		Name:       nat.ModuleName,
		ProviderID: mod.node.Identity(),
	}
}
