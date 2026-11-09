// Command driver tells one story across node1 and node2: a consumer app on
// node1 follows the echo-follow service with reach=swarm before any provider
// exists; a provider app starts on node2, the follow shows it, and the
// consumer calls it; the provider stops, and the follow removes it.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

const service = "echo-follow"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_discover_follow": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func story(path string, facts map[string]bool) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	n1, n2 := nodes["node1"], nodes["node2"]
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)

	// the consumer: an app on node1 that node1's admin grants discovery of echo-follow
	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}
	f, err := follow(ctx, consumer, services.ReachSwarm, service)
	if err != nil {
		return err
	}
	defer f.stop()

	ev, err := f.next()
	empty := err == nil && ev.Initial != nil && ev.Initial.Complete
	if err := step(facts, "follow_starts_empty", empty, "first follow event %+v, err %v; want a complete initial outcome with no offering", ev, err); err != nil {
		return err
	}

	// the provider: an app on node2 that starts after the follow and serves echo
	a, err := register(ctx, n2, serveApps)
	if err != nil {
		return err
	}
	provider, err := startEcho(ctx, a, service)
	if err != nil {
		return err
	}
	defer provider.stop()

	ev, err = f.next()
	shown := err == nil && ev.Update != nil && bool(ev.Update.Available) &&
		ev.Update.ProviderID.IsEqual(provider.id) && string(ev.Update.Name) == service
	if err := step(facts, "follow_shows_the_provider", shown, "follow event %+v, err %v; want %v offering %v", ev, err, provider.id, service); err != nil {
		return err
	}

	// note: the follow does not wait for the provider's relay contract to
	// reach node1 (the route_not_found race), so the call waits until node1
	// names node2 as the provider's host.
	hosts, err := awaitHost(ctx, connect(n1.Endpoint, n1.Token), provider.id, node2)
	if err := step(facts, "node1_knows_the_host", err == nil && len(hosts) == 1, "apphost.hosts on node1 named %v (err %v); want node2 %v alone", hosts, err, node2); err != nil {
		return err
	}
	answer, err := call(ctx, consumer, ev.Update.ProviderID, "hello")
	if err := step(facts, "provider_echoed", err == nil && answer == "hello", "echo answered %q, err %v; want \"hello\"", answer, err); err != nil {
		return err
	}
	if err := step(facts, "provider_saw_the_consumer", provider.servedOnlyBy(consumer.id), "the provider served %v; want %v alone", provider.served(), consumer.id); err != nil {
		return err
	}

	provider.stop()
	ev, err = f.next()
	return step(facts, "follow_removes_the_provider", removes(ev, err, provider.id), "follow event %+v, err %v; want the removal of %v offering %v", ev, err, provider.id, service)
}

// removes reports whether ev removes exactly the provider's offering of service.
func removes(ev servicescli.Event, err error, provider *astral.Identity) bool {
	if err != nil || len(ev.Removed) != 1 {
		return false
	}
	k := ev.Removed[0]
	return k.ProviderID.IsEqual(provider) && string(k.Name) == service
}
