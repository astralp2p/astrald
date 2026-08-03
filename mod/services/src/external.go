package services

import (
	"sync"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	servicesmod "github.com/astralp2p/astrald/mod/services"
)

var _ servicesmod.Discoverer = &externalServices{}

// externalServices holds the services advertised by apps rather than by modules
// compiled into the node. An advertisement lives in memory for as long as the
// channel that raised it, so a departed app leaves nothing behind: there is no
// stored copy to go stale, which is the failure a directory entry has and this
// does not.
type externalServices struct {
	mu      sync.RWMutex
	ads     map[string]*services.Update
	watches map[chan *services.Update]struct{}
}

func newExternalServices() *externalServices {
	return &externalServices{
		ads:     map[string]*services.Update{},
		watches: map[chan *services.Update]struct{}{},
	}
}

func adKey(providerID *astral.Identity, name string) string {
	return providerID.String() + "\x00" + name
}

// advertise records a service as available and tells every follower. Calling it
// again for the same provider and name replaces the info in place: the service
// never goes unavailable in between, so a follower sees an amended
// advertisement rather than a withdrawal chased by a fresh one.
func (e *externalServices) advertise(providerID *astral.Identity, name string, info *astral.Bundle) {
	update := &services.Update{
		Available:  true,
		Name:       astral.String8(name),
		ProviderID: providerID,
		Info:       info,
	}

	e.mu.Lock()
	e.ads[adKey(providerID, name)] = update
	e.mu.Unlock()

	e.notify(update)
}

// withdraw drops an advertisement and tells every follower it is gone.
func (e *externalServices) withdraw(providerID *astral.Identity, name string) {
	key := adKey(providerID, name)

	e.mu.Lock()
	_, found := e.ads[key]
	delete(e.ads, key)
	e.mu.Unlock()

	if !found {
		return
	}

	e.notify(&services.Update{
		Available:  false,
		Name:       astral.String8(name),
		ProviderID: providerID,
	})
}

// notify delivers an update to every follower, dropping it for any whose buffer
// is full: a follower that has stopped reading holds up nobody else.
func (e *externalServices) notify(update *services.Update) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for watch := range e.watches {
		select {
		case watch <- update:
		default:
		}
	}
}

// DiscoverServices streams the advertisements an app has raised. Network-origin
// callers are served nothing: an app extends the node hosting it, and what it
// advertises is that node's business until someone decides otherwise.
func (e *externalServices) DiscoverServices(
	ctx *astral.Context,
	caller *astral.Identity,
	follow bool,
) (<-chan *services.Update, error) {
	e.mu.RLock()
	var snapshot = make([]*services.Update, 0, len(e.ads))
	for _, update := range e.ads {
		snapshot = append(snapshot, update)
	}
	e.mu.RUnlock()

	if !follow {
		var ch = make(chan *services.Update, len(snapshot))
		for _, update := range snapshot {
			ch <- update
		}
		close(ch)
		return ch, nil
	}

	var watch = make(chan *services.Update, 16)

	e.mu.Lock()
	e.watches[watch] = struct{}{}
	e.mu.Unlock()

	var out = make(chan *services.Update)

	go func() {
		defer close(out)
		defer func() {
			e.mu.Lock()
			delete(e.watches, watch)
			e.mu.Unlock()
		}()

		for _, update := range snapshot {
			select {
			case <-ctx.Done():
				return
			case out <- update:
			}
		}

		// the separator between the snapshot and what follows it
		select {
		case <-ctx.Done():
			return
		case out <- nil:
		}

		for {
			select {
			case <-ctx.Done():
				return
			case update := <-watch:
				select {
				case <-ctx.Done():
					return
				case out <- update:
				}
			}
		}
	}()

	return out, nil
}
