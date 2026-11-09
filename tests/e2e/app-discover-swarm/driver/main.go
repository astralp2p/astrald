// Command driver tells one story across node1 and node2: a provider app on
// node2 advertises the echo-swarm service and serves the echo op; a consumer
// app on node1 granted discovery of echo-swarm finds the provider with
// reach=swarm and calls it on node2.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const service = "echo-swarm"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_discover_swarm": facts})
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

	// the provider: an app on node2 that advertises echo-swarm and serves echo
	a, err := register(ctx, n2, serveApps)
	if err != nil {
		return err
	}
	provider, err := startEcho(ctx, a, service)
	if err != nil {
		return err
	}
	defer provider.stop()

	// the consumer: an app on node1 that node1's admin grants discovery of echo-swarm
	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}

	d := discoverOnce(ctx, consumer, services.ReachSwarm, service)
	found := d.complete() && len(d.providers) == 1 && d.providers[0].IsEqual(provider.id)
	if err := step(facts, "consumer_found_the_provider", found, "swarm discovery: providers %v, outcome %+v, err %v; want %v alone", d.providers, d.outcome, d.err, provider.id); err != nil {
		return err
	}

	// note: discovery does not wait for the provider's relay contract to reach
	// node1 (the route_not_found race), so the call waits until node1 names
	// node2 as the provider's host.
	hosts, err := awaitHost(ctx, connect(n1.Endpoint, n1.Token), provider.id, node2)
	if err := step(facts, "node1_knows_the_host", err == nil && len(hosts) == 1, "apphost.hosts on node1 named %v (err %v); want node2 %v alone", hosts, err, node2); err != nil {
		return err
	}

	answer, err := call(ctx, consumer, d.providers[0], "hello")
	if err := step(facts, "provider_echoed", err == nil && answer == "hello", "echo answered %q, err %v; want \"hello\"", answer, err); err != nil {
		return err
	}
	return step(facts, "provider_saw_the_consumer", provider.servedOnlyBy(consumer.id), "the provider served %v; want %v alone", provider.served(), consumer.id)
}
