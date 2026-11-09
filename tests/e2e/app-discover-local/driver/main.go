// Command driver tells one story on node1: a provider app advertises the
// echo-local service and serves the echo op; a consumer app granted discovery
// of echo-local finds the provider and calls it; an app without the grant is
// refused the discovery.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const service = "echo-local"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_discover_local": facts})
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
	n1 := nodes["node1"]
	ctx := astral.NewContext(nil)

	// the provider: an app on node1 that advertises echo-local and serves echo
	a, err := register(ctx, n1, serveApps)
	if err != nil {
		return err
	}
	provider, err := startEcho(ctx, a, service)
	if err != nil {
		return err
	}
	defer provider.stop()

	// the consumer: an app on node1 the node's admin grants discovery of echo-local
	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}

	d := discoverOnce(ctx, consumer, services.ReachLocal, service)
	found := d.complete() && len(d.providers) == 1 && d.providers[0].IsEqual(provider.id)
	if err := step(facts, "consumer_found_the_provider", found, "discovery: providers %v, outcome %+v, err %v; want %v alone", d.providers, d.outcome, d.err, provider.id); err != nil {
		return err
	}

	answer, err := call(ctx, consumer, d.providers[0], "hello")
	if err := step(facts, "provider_echoed", err == nil && answer == "hello", "echo answered %q, err %v; want \"hello\"", answer, err); err != nil {
		return err
	}
	if err := step(facts, "provider_saw_the_consumer", provider.servedOnlyBy(consumer.id), "the provider served %v; want %v alone", provider.served(), consumer.id); err != nil {
		return err
	}

	// a stranger: an app on node1 with no discovery grant
	stranger, err := register(ctx, n1)
	if err != nil {
		return err
	}
	d = discoverOnce(ctx, stranger, services.ReachLocal, service)
	refused := d.err != nil && strings.Contains(d.err.Error(), "not permitted to discover") && len(d.providers) == 0
	if err := step(facts, "stranger_refused", refused, "discovery without a grant: providers %v, err %v; want a refusal", d.providers, d.err); err != nil {
		return err
	}
	return nil
}
