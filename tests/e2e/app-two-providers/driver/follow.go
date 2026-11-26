package main

import (
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

// permitAfterFollow opens a swarm follow of privateService for an app without
// a permit, then has it request the permit: the open follow shows the private provider
// on node1 and the one on node2 within 2 s, without a reopen.
func permitAfterFollow(ctx *astral.Context, n1, n2 node, facts map[string]bool) error {
	hidNear, err := startProviderOf(ctx, n1, privateService, false)
	if err != nil {
		return err
	}
	defer hidNear.stop()
	hidFar, err := startProviderOf(ctx, n2, privateService, false)
	if err != nil {
		return err
	}
	defer hidFar.stop()

	late, err := register(ctx, n1)
	if err != nil {
		return err
	}
	fctx, cancel := ctx.WithCancel()
	defer cancel()
	events, err := servicescli.New(nil, late.client).DiscoverIn(fctx, services.ReachSwarm, []string{privateService}, true)
	if err != nil {
		return err
	}
	before := readInitial(events, 60*time.Second)
	empty := before.initial && len(before.providers) == 0 && slices.Contains(before.notGranted, privateService)
	if err := step(facts, "follow_starts_without_private_providers", empty,
		"follow of %v before the permit: initial %v, providers %v, not granted %v; want no provider and %v not granted",
		privateService, before.initial, before.providers, before.notGranted, privateService); err != nil {
		return err
	}

	if err := requestDiscovery(ctx, late, privateService); err != nil {
		return err
	}
	seen := awaitProviders(events, 2*time.Second, hidNear.id, hidFar.id)
	return step(facts, "permit_shows_both_without_reopen", holdsBoth(seen, hidNear.id, hidFar.id),
		"follow of %v after the permit showed %v within 2 s; want %v and %v", privateService, seen, hidNear.id, hidFar.id)
}

// followStart is what a follow carried up to its initial outcome.
type followStart struct {
	initial    bool
	providers  []*astral.Identity
	notGranted []string
}

func readInitial(events <-chan servicescli.Event, d time.Duration) followStart {
	var s followStart
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-events:
			switch {
			case !ok:
				return s
			case ev.NotGranted != nil:
				s.notGranted = ev.NotGranted
			case ev.Update != nil && bool(ev.Update.Available):
				s.providers = append(s.providers, ev.Update.ProviderID)
			case ev.Initial != nil:
				s.initial = true
				return s
			}
		case <-deadline:
			return s
		}
	}
}

// awaitProviders reads the follow until it showed every wanted provider or d
// passed, and returns the providers it showed.
func awaitProviders(events <-chan servicescli.Event, d time.Duration, want ...*astral.Identity) []*astral.Identity {
	var seen []*astral.Identity
	deadline := time.After(d)
	for {
		missing := false
		for _, w := range want {
			missing = missing || !slices.ContainsFunc(seen, w.IsEqual)
		}
		if !missing {
			return seen
		}
		select {
		case ev, ok := <-events:
			if !ok {
				return seen
			}
			if ev.Update != nil && bool(ev.Update.Available) {
				seen = append(seen, ev.Update.ProviderID)
			}
		case <-deadline:
			return seen
		}
	}
}
