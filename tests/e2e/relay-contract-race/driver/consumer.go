package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// directory names the node of each provider app, by the app's identity.
type directory = sig.Map[string, string]

// consumer is a consumer app on one node with an open follow of the service.
type consumer struct {
	name   string
	app    *app
	events <-chan servicescli.Event
	stop   func()
}

// openConsumer registers a consumer app on the named node, permits it
// discovery of the service, and opens its swarm follow. It returns once the
// follow's initial outcome arrived complete.
func openConsumer(ctx *astral.Context, name string, n node) (*consumer, error) {
	a, err := register(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("registering the consumer on %s: %w", name, err)
	}
	if err := requestDiscovery(ctx, a, service); err != nil {
		return nil, fmt.Errorf("requesting discovery for the consumer on %s: %w", name, err)
	}
	fctx, cancel := ctx.WithCancel()
	events, err := servicescli.New(nil, a.client).DiscoverIn(fctx, services.ReachSwarm, []string{service}, true)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("the follow of the consumer on %s: %w", name, err)
	}
	c := &consumer{name: name, app: a, events: events, stop: cancel}
	if err := c.awaitInitial(); err != nil {
		cancel()
		return nil, fmt.Errorf("the follow of the consumer on %s: %w", name, err)
	}
	return c, nil
}

// awaitInitial reads the follow up to its initial outcome, which must be complete.
func (c *consumer) awaitInitial() error {
	timeout := time.After(converge)
	for {
		select {
		case ev, ok := <-c.events:
			switch {
			case !ok:
				return errors.New("the follow ended before its initial outcome")
			case ev.Err != nil:
				return ev.Err
			case ev.NotGranted != nil:
				return fmt.Errorf("not granted: %v", ev.NotGranted)
			case ev.Initial != nil && !ev.Initial.Complete:
				return fmt.Errorf("incomplete initial outcome: %v", ev.Initial.Incomplete)
			case ev.Initial != nil:
				return nil
			}
		case <-timeout:
			return fmt.Errorf("no initial outcome within %v", converge)
		}
	}
}

// callOnFirstUpdate reads the follow and calls each remote provider once, on
// its first available update, until it called want providers, the follow
// ended, or twice converge passed. It returns every call's result.
//
// why no retry: the story proves that the first call routes.
func (c *consumer) callOnFirstUpdate(ctx *astral.Context, dir *directory, want int) []pair {
	results := make(chan pair, want)
	called := map[string]bool{}
	timeout := time.After(2 * converge)
	var wg sync.WaitGroup
	for len(called) < want {
		var ev servicescli.Event
		ok := false
		select {
		case ev, ok = <-c.events:
		case <-timeout:
		}
		if !ok {
			break
		}
		provider, node := c.remoteProvider(ev, dir)
		if provider == nil || called[node] {
			continue
		}
		called[node] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- c.call(ctx, provider, node)
		}()
	}
	wg.Wait()
	close(results)

	var pairs []pair
	for p := range results {
		pairs = append(pairs, p)
	}
	return pairs
}

// remoteProvider returns the provider and its node when ev makes a provider
// on another node available.
func (c *consumer) remoteProvider(ev servicescli.Event, dir *directory) (*astral.Identity, string) {
	if ev.Update == nil || !bool(ev.Update.Available) || ev.Update.ProviderID == nil {
		return nil, ""
	}
	node, ok := dir.Get(ev.Update.ProviderID.String())
	if !ok || node == c.name {
		return nil, ""
	}
	return ev.Update.ProviderID, node
}

// call calls provider:echo once with the consumer's node name and times it.
func (c *consumer) call(ctx *astral.Context, provider *astral.Identity, node string) pair {
	start := time.Now()
	answer, err := echo(ctx, c.app, provider, c.name)
	p := pair{Consumer: c.name, Provider: node, Ms: time.Since(start).Milliseconds()}
	switch {
	case err != nil:
		p.Err = err.Error()
	case answer != c.name:
		p.Err = fmt.Sprintf("answered %q; want %q", answer, c.name)
	default:
		p.Answered = true
	}
	return p
}
