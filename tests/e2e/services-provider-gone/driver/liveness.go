package main

import (
	"fmt"
	"syscall"
	"time"
)

// Liveness bounds with the node's defaults: a 60 s probe interval, a 10 s
// request timeout and 2 consecutive unanswered asks. A provider that stops
// answering right after an answer is retired about 130 s later.
// Spec: .ai/system/protocols/services/ops/services.advertise.md#liveness
const (
	lateAnswer   = 12 * time.Second  // past the 10 s request timeout
	slowRetired  = 170 * time.Second // probes at 60 s and 120 s, each timing out 10 s later
	hungRetired  = 150 * time.Second
	readvertised = 30 * time.Second // the keeper's re-advertise, then the node's ask
)

// livenessStory goes on from an empty follow f of prov's service:
//  1. a provider that answers its next 2 asks 12 s late is retired: the follow
//     removes it (slow_provider_is_retired);
//  2. its keeper advertises again and the follow shows it
//     (slow_provider_advertises_again);
//  3. the provider is stopped with SIGSTOP; the follow removes it within
//     150 s (hung_provider_is_retired);
//  4. after SIGCONT its keeper advertises again and the follow shows it
//     (resumed_provider_advertises_again);
//  5. a clean stop removes it (liveness_stop_removes_the_provider).
func livenessStory(f *follower, prov *app, facts map[string]bool) error {
	c, err := startChild(prov, "SPG_SLOW_ASKS=2")
	if err != nil {
		return err
	}
	defer c.kill()
	ev, err := f.next()
	if err := step(facts, "slow_provider_shown", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v", ev, err, prov.id, service); err != nil {
		return err
	}

	ev, err = f.nextWithin(slowRetired)
	if err := step(facts, "slow_provider_is_retired", removes(ev, err, prov.id), "follow event %+v, err %v; want the removal of %v offering %v within %v", ev, err, prov.id, service, slowRetired); err != nil {
		return err
	}
	ev, err = f.nextWithin(readvertised)
	if err := step(facts, "slow_provider_advertises_again", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v again within %v", ev, err, prov.id, service, readvertised); err != nil {
		return err
	}

	if err := c.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		return fmt.Errorf("stopping the provider with SIGSTOP: %v", err)
	}
	start := time.Now()
	ev, err = f.nextWithin(hungRetired)
	if err := step(facts, "hung_provider_is_retired", removes(ev, err, prov.id), "follow event %+v, err %v after %v; want the removal of %v offering %v within %v", ev, err, time.Since(start), prov.id, service, hungRetired); err != nil {
		return err
	}

	if err := c.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		return fmt.Errorf("resuming the provider with SIGCONT: %v", err)
	}
	ev, err = f.nextWithin(readvertised)
	if err := step(facts, "resumed_provider_advertises_again", shows(ev, err, prov.id), "follow event %+v, err %v; want %v offering %v again within %v", ev, err, prov.id, service, readvertised); err != nil {
		return err
	}

	if err := c.stop(); err != nil {
		return fmt.Errorf("stopping the provider cleanly: %v", err)
	}
	ev, err = f.next()
	return step(facts, "liveness_stop_removes_the_provider", removes(ev, err, prov.id), "follow event %+v, err %v; want the removal of %v offering %v", ev, err, prov.id, service)
}
