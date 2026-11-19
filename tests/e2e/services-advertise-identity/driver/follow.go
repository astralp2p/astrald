package main

import (
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

// follow is an open services.discover follow whose events the story reads.
type follow struct {
	events <-chan servicescli.Event
	cancel func()
}

func openFollow(ctx *astral.Context, a *app, reach services.Reach, names ...string) (*follow, error) {
	fctx, cancel := ctx.WithCancel()
	events, err := servicescli.New(nil, a.client).DiscoverIn(fctx, reach, names, true)
	if err != nil {
		cancel()
		return nil, err
	}
	return &follow{events: events, cancel: cancel}, nil
}

// await reads events until one matches, and reports whether one did within d.
func (f *follow) await(d time.Duration, match func(servicescli.Event) bool) bool {
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-f.events:
			if !ok {
				return false
			}
			if match(ev) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func updateFrom(provider *astral.Identity) func(servicescli.Event) bool {
	return func(ev servicescli.Event) bool {
		return ev.Update != nil && bool(ev.Update.Available) && ev.Update.ProviderID.IsEqual(provider)
	}
}

// removalOf matches a removal of provider's offering of name with cause.
func removalOf(provider *astral.Identity, name string, cause services.RemovalCause) func(servicescli.Event) bool {
	return func(ev servicescli.Event) bool {
		if ev.RemovedCause != cause {
			return false
		}
		for _, k := range ev.Removed {
			if k.ProviderID.IsEqual(provider) && string(k.Name) == name {
				return true
			}
		}
		return false
	}
}

// closedWithin reports whether done closes within d.
func closedWithin(done <-chan struct{}, d time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
