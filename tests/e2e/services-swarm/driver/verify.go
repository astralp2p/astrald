package main

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/services"
)

// verify checks the swarm property on its own, with identities the driver
// never used: a provider on node2 is found from node1 only with reach=swarm,
// and evaluates the asking app.
func verify(w *world) error {
	app, f, err := w.app(w.admin1, w.n1)
	if err != nil {
		return err
	}
	provider, c2, err := w.app(w.admin2, w.n2, serveApps)
	if err != nil {
		return err
	}
	if err := w.grant(app, []string{"oracle-check"}); err != nil {
		return err
	}
	seen := &evaluated{}
	b, err := c2.Advertise(w.ctx, []string{"oracle-check"}, seen.offer)
	if err != nil {
		return err
	}
	defer b.Close()

	if r := oneShot(f.Discover(w.ctx, []string{"oracle-check"}, false)); !r.complete() || len(r.providers) != 0 {
		return fmt.Errorf("local reach found a provider on another node: %+v", r)
	}
	r := oneShot(f.DiscoverIn(w.ctx, services.ReachSwarm, []string{"oracle-check"}, false))
	if !r.complete() || len(r.providers) != 1 || r.providers[0] != provider.String()+"/oracle-check" {
		return fmt.Errorf("reach=swarm: %+v; want node2's provider %v", r, provider)
	}
	if !seen.saw(app) {
		return fmt.Errorf("node2's provider evaluated %v; want the app %v", seen.callers, app)
	}
	fmt.Println("oracle: node2's provider found from node1 with reach=swarm, evaluated for the app")
	return nil
}
