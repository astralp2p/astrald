package main

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

// causeUnavailable marks an offering its provider answered unavailable; it
// is no node's removal cause.
const causeUnavailable = "unavailable"

// follow follows the service with reach=swarm, reopening the follow with the
// SDK's backoff, and calls every provider it reports until ctx ends.
func (m *mesh) follow(ctx *astral.Context, wg *sync.WaitGroup) {
	events, err := servicescli.New(nil, m.client).FollowReconnecting(ctx, services.ReachSwarm, []string{m.service})
	if err != nil {
		m.rec.update(func(st *appState) { st.FollowErr = err.Error() })
		return
	}
	for ev := range events {
		m.apply(ctx, wg, ev, unixSeconds(time.Now()))
	}
}

// apply records one follow event at time t.
func (m *mesh) apply(ctx *astral.Context, wg *sync.WaitGroup, ev servicescli.Event, t float64) {
	switch {
	case ev.Err != nil:
		if ctx.Err() == nil {
			m.rec.update(func(st *appState) { st.FollowErr = ev.Err.Error() })
		}
	case ev.Initial != nil:
		m.rec.update(func(st *appState) {
			st.FollowOpens++
			st.Initial = &initialRecord{T: t, Complete: ev.Initial.Complete,
				Incomplete: append([]string{}, ev.Initial.Incomplete...)}
		})
	case ev.Update != nil:
		m.offer(ctx, wg, ev.Update, t)
	case len(ev.Removed) > 0:
		m.retract(ev.Removed, causeName(ev.RemovedCause), t)
	case ev.NotGranted != nil:
		m.rec.update(func(st *appState) { st.NotGranted = append([]string{}, ev.NotGranted...) })
	}
}

// offer records a provider's update and starts calling a provider first seen.
func (m *mesh) offer(ctx *astral.Context, wg *sync.WaitGroup, u *services.Update, t float64) {
	if u.ProviderID == nil || string(u.Name) != m.service || u.ProviderID.String() == m.self {
		return
	}
	id := u.ProviderID.String()
	if !bool(u.Available) {
		m.rec.remove(id, t, causeUnavailable)
		return
	}
	if m.rec.report(kindApp, id, t, false) {
		m.spawnCaller(ctx, wg, kindApp, u.ProviderID)
	}
}

// retract records the removal of other providers' offerings.
func (m *mesh) retract(keys []*services.OfferingKey, cause string, t float64) {
	for _, k := range keys {
		if k == nil || k.ProviderID == nil || string(k.Name) != m.service || k.ProviderID.String() == m.self {
			continue
		}
		m.rec.remove(k.ProviderID.String(), t, cause)
	}
}

// causeName is the spec name of a removal cause; zero, a reopened follow
// that no longer shows the offering, is "none".
func causeName(c services.RemovalCause) string {
	if c == 0 {
		return "none"
	}
	return c.String()
}
