package main

import (
	"fmt"
	"slices"
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// story opens a consumer's follow on every node, then starts a provider on
// every node while the consumers call each remote provider on its first
// available update, and records the 12 calls, what each provider served, and
// the hosts each node then holds. It fails only when the flow cannot run.
func story(ctx *astral.Context, s *session, r *report) error {
	consumers, err := openConsumers(ctx, s, r)
	defer stopConsumers(consumers)
	if err := step(r, "follows_open_and_complete", err == nil, "%v", err); err != nil {
		return err
	}

	dir := &directory{}
	results := make(chan []pair, len(consumers))
	var wg sync.WaitGroup
	for _, c := range consumers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- c.callOnFirstUpdate(ctx, dir, len(names)-1)
		}()
	}

	providers, err := startProviders(ctx, s, dir, r)
	defer stopProviders(providers)
	if err != nil {
		// why: an ended follow ends its consumer's loop
		stopConsumers(consumers)
	}
	wg.Wait()
	close(results)
	if err := step(r, "providers_advertised", err == nil, "%v", err); err != nil {
		return err
	}

	for ps := range results {
		r.Pairs = append(r.Pairs, ps...)
	}
	for name, p := range providers {
		r.Served[name] = p.served()
	}
	r.HostsMissing = missingHosts(ctx, s, r)
	r.Steps["consumer_nodes_hold_the_contracts"] = len(r.HostsMissing) == 0
	return nil
}

func openConsumers(ctx *astral.Context, s *session, r *report) ([]*consumer, error) {
	var consumers []*consumer
	for _, name := range names {
		c, err := openConsumer(ctx, name, s.Nodes[name])
		if err != nil {
			return consumers, err
		}
		r.Consumers[name] = c.app.id.String()
		consumers = append(consumers, c)
	}
	return consumers, nil
}

func stopConsumers(consumers []*consumer) {
	for _, c := range consumers {
		c.stop()
	}
}

// startProviders registers and serves a provider app on every node, one
// after another, and names each one's node in dir before it advertises.
//
// why before it advertises: a consumer finds the node of an update's
// provider in dir.
func startProviders(ctx *astral.Context, s *session, dir *directory, r *report) (map[string]*echoApp, error) {
	providers := map[string]*echoApp{}
	for _, name := range names {
		a, err := register(ctx, s.Nodes[name], serveApps)
		if err != nil {
			return providers, fmt.Errorf("registering the provider on %s: %w", name, err)
		}
		dir.Set(a.id.String(), name)
		r.Providers[name] = a.id.String()
		e, err := startEcho(ctx, a, service)
		if err != nil {
			return providers, fmt.Errorf("the provider on %s: %w", name, err)
		}
		providers[name] = e
	}
	return providers, nil
}

func stopProviders(providers map[string]*echoApp) {
	for _, p := range providers {
		p.stop()
	}
}

// missingHosts lists every node whose apphost.hosts does not name a remote
// provider's node: a contract the node did not pull for its call.
func missingHosts(ctx *astral.Context, s *session, r *report) []string {
	var missing []string
	for _, at := range names {
		for _, of := range names {
			if at == of {
				continue
			}
			hosts, err := hostsOf(ctx, admin(s.Nodes[at]), r.Providers[of])
			if err != nil || !slices.Contains(hosts, s.Nodes[of].Identity) {
				missing = append(missing, fmt.Sprintf("%s holds %v for the provider on %s (err %v)", at, hosts, of, err))
			}
		}
	}
	return missing
}
