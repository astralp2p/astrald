package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

// drive runs every scenario, stops at the first failure, and prints which
// steps passed as JSON facts.
func drive(w *world) error {
	facts := map[string]bool{}
	err := scenarios(w, facts)
	out, _ := json.Marshal(map[string]any{"services_swarm": facts})
	fmt.Println(string(out))
	return err
}

func scenarios(w *world, facts map[string]bool) error {
	app, f, err := w.app(w.admin1, w.n1)
	if err != nil {
		return err
	}
	_, c1, err := w.app(w.admin1, w.n1, serveApps)
	if err != nil {
		return err
	}
	_, c2, err := w.app(w.admin2, w.n2, serveApps)
	if err != nil {
		return err
	}

	// node1: a two-service provider built on NewProvider.
	local := &evaluated{}
	p1 := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		"player": func(ctx *astral.Context, c *astral.Identity) (*services.Update, error) {
			return local.offer(ctx, c, "player")
		},
		"wallet": func(ctx *astral.Context, c *astral.Identity) (*services.Update, error) {
			return local.offer(ctx, c, "wallet")
		},
	})
	if err := p1.Serve(w.ctx, c1); err != nil {
		return err
	}
	defer p1.Close()
	// node2: one player.
	remote := &evaluated{}
	b2, err := c2.Advertise(w.ctx, []string{"player"}, remote.offer)
	if err != nil {
		return err
	}

	if err := w.grant(app, []string{"player", "wallet"}, w.node1); err != nil {
		return err
	}
	r := oneShot(f.DiscoverIn(w.ctx, services.ReachSwarm, []string{"player"}, false))
	if err := step(facts, "swarm_refused_beyond_scope", expect(r.err != nil, "a discovery beyond the app's scope was answered: %v", r.providers)); err != nil {
		return err
	}

	r = oneShot(f.Discover(w.ctx, []string{"player", "wallet"}, false))
	if err := step(facts, "local_two_service_binding", expect(r.complete() && len(r.providers) == 2, "local discovery: %+v", r)); err != nil {
		return err
	}

	if err := w.grant(app, []string{"player", "wallet", "slow", "none"}); err != nil {
		return err
	}
	r = oneShot(f.DiscoverIn(w.ctx, services.ReachSwarm, []string{"player"}, false))
	if err := step(facts, "swarm_one_shot_complete", expect(r.complete() && len(r.providers) == 2, "swarm discovery: %+v", r)); err != nil {
		return err
	}
	if err := step(facts, "member_evaluates_the_app", expect(remote.saw(app) && !remote.saw(w.node1), "node2's provider evaluated %v", remote.callers)); err != nil {
		return err
	}

	return follow(w, f, followCtx{facts: facts, local: local, p1: p1, b2: b2, c2: c2, remote: remote})
}

type followCtx struct {
	facts  map[string]bool
	local  *evaluated
	p1     *servicescli.Provider
	b2     *servicescli.Binding
	c2     *servicescli.Client
	remote *evaluated
}

func follow(w *world, f *servicescli.Client, fc followCtx) error {
	watch, err := f.WatchIn(w.ctx, services.ReachSwarm, []string{"player", "wallet"})
	if err != nil {
		return err
	}
	// two services on node1, one on node2
	if err := step(fc.facts, "swarm_follow_initial", holdsN(watch, 3)); err != nil {
		return err
	}
	all := watchKeys(watch)

	before := infoOf(watch)
	fc.local.gen.Add(1)
	if err := fc.p1.ChangeAll(); err != nil {
		return err
	}
	if err := step(fc.facts, "change_gives_a_new_view", waitFor(watch, func() bool { return infoOf(watch) != before })); err != nil {
		return err
	}

	fc.b2.Close()
	if err := step(fc.facts, "member_provider_loss_removes", holdsN(watch, 2)); err != nil {
		return err
	}
	b2, err := fc.c2.Advertise(w.ctx, []string{"player"}, fc.remote.offer)
	if err != nil {
		return err
	}
	defer b2.Close()
	if err := step(fc.facts, "member_provider_returns", holds(watch, all...)); err != nil {
		return err
	}
	return outcomes(w, f, fc.facts)
}

func outcomes(w *world, f *servicescli.Client, facts map[string]bool) error {
	_, slowCli, err := w.app(w.admin1, w.n1, serveApps)
	if err != nil {
		return err
	}
	block := make(chan struct{})
	defer close(block)
	b, err := slowCli.Advertise(w.ctx, []string{"slow"}, func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
		<-block
		return nil, nil
	})
	if err != nil {
		return err
	}
	defer b.Close()
	r := oneShot(f.Discover(w.ctx, []string{"slow"}, false))
	inc := r.outcome != nil && !r.outcome.Complete && len(r.outcome.Incomplete) == 1 && r.outcome.Incomplete[0] == "slow"
	if err := step(facts, "silent_provider_incomplete", expect(r.err == nil && inc, "%+v", r)); err != nil {
		return err
	}

	start := time.Now()
	r = oneShot(f.DiscoverIn(w.ctx, services.ReachSwarm, []string{"none"}, false))
	return step(facts, "unregistered_bare_eos", expect(r.complete() && len(r.providers) == 0 && time.Since(start) < 3*time.Second, "%+v after %v", r, time.Since(start)))
}

func watchKeys(w *servicescli.Watcher) []string {
	var out []string
	for _, u := range w.Offerings() {
		out = append(out, u.ProviderID.String()+"/"+string(u.Name))
	}
	return out
}

func holdsN(w *servicescli.Watcher, n int) error {
	return waitFor(w, func() bool { return len(w.Offerings()) == n })
}

func waitFor(w *servicescli.Watcher, cond func() bool) error {
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case <-w.Changed():
		case <-w.Done():
			return fmt.Errorf("follow ended: %v", w.Err())
		case <-deadline:
			return fmt.Errorf("follow holds %v", watchKeys(w))
		}
	}
	return nil
}

func infoOf(w *servicescli.Watcher) string {
	out := ""
	for _, u := range w.Offerings() {
		if u.Info != nil {
			out += fmt.Sprint(u.Info.Objects())
		}
	}
	return out
}
