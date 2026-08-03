package services

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	servicesmod "github.com/astralp2p/astrald/mod/services"
)

var _ servicesmod.Discoverer = &externalServices{}

// externalServices holds the services advertised by apps rather than by
// modules compiled into the node. An advertisement lives for as long as the
// channel that raised it, so a departed app leaves no stored copy to go stale.
type externalServices struct {
	ads     sig.Map[string, *services.Update]
	watches sig.Set[chan *services.Update]
}

func newExternalServices() *externalServices {
	return &externalServices{}
}

func adKey(providerID *astral.Identity, name string) string {
	return providerID.String() + "\x00" + name
}

// advertise records a service as available and tells every follower. Repeating
// it for the same provider and name replaces the info in place: the service
// stays available, so a follower sees an amended advertisement rather than a
// withdrawal and a fresh one.
func (e *externalServices) advertise(providerID *astral.Identity, name string, info *astral.Bundle) {
	update := &services.Update{
		Available:  true,
		Name:       astral.String8(name),
		ProviderID: providerID,
		Info:       info,
	}

	e.ads.Replace(adKey(providerID, name), update)
	e.notify(update)
}

// withdraw drops an advertisement and tells every follower it is gone.
// Withdrawing what is already gone tells nobody anything.
func (e *externalServices) withdraw(providerID *astral.Identity, name string) {
	if _, found := e.ads.Delete(adKey(providerID, name)); !found {
		return
	}

	e.notify(&services.Update{
		Available:  false,
		Name:       astral.String8(name),
		ProviderID: providerID,
	})
}

// notify delivers an update to every follower, dropping it for any whose
// buffer is full: a follower that stopped reading holds up nobody else.
func (e *externalServices) notify(update *services.Update) {
	for _, watch := range e.watches.Clone() {
		select {
		case watch <- update:
		default:
		}
	}
}

func (e *externalServices) snapshot() []*services.Update {
	var out []*services.Update
	for _, update := range e.ads.Clone() {
		out = append(out, update)
	}
	return out
}

// DiscoverServices streams the advertisements apps have raised. Network-origin
// callers are served nothing: an app extends the node hosting it.
func (e *externalServices) DiscoverServices(
	ctx *astral.Context,
	caller *astral.Identity,
	follow bool,
) (<-chan *services.Update, error) {
	snapshot := e.snapshot()

	if !follow {
		var ch = make(chan *services.Update, len(snapshot))
		for _, update := range snapshot {
			ch <- update
		}
		close(ch)
		return ch, nil
	}

	var watch = make(chan *services.Update, 16)
	_ = e.watches.Add(watch)

	var out = make(chan *services.Update)

	go func() {
		defer close(out)
		defer e.watches.Remove(watch)

		for _, update := range snapshot {
			if err := sig.Send(ctx, out, update); err != nil {
				return
			}
		}

		// the separator between the snapshot and what follows it
		if err := sig.Send(ctx, out, nil); err != nil {
			return
		}

		for {
			update, err := sig.Recv(ctx, watch)
			if err != nil {
				return
			}
			if err = sig.Send(ctx, out, update); err != nil {
				return
			}
		}
	}()

	return out, nil
}
