// Command driver tells one story across node1 and node2: two provider apps,
// one on each node, advertise the echo-pair service and serve the echo op; a
// consumer app on node1 granted discovery of echo-pair finds both with
// reach=swarm and calls each, and each provider answers its own call alone.
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

const service = "echo-pair"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_two_providers": facts})
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
	ctx := astral.NewContext(nil)

	// the providers: an app on node1 and an app on node2, each advertising echo-pair
	near, err := startProvider(ctx, n1)
	if err != nil {
		return err
	}
	defer near.stop()
	far, err := startProvider(ctx, n2)
	if err != nil {
		return err
	}
	defer far.stop()

	// the consumer: an app on node1 that node1's admin grants discovery of echo-pair
	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}

	d := discoverOnce(ctx, consumer, services.ReachSwarm, service)
	both := d.complete() && len(d.providers) == 2 && holdsBoth(d.providers, near.id, far.id)
	if err := step(facts, "consumer_found_both", both, "swarm discovery: providers %v, outcome %+v, err %v; want %v and %v", d.providers, d.outcome, d.err, near.id, far.id); err != nil {
		return err
	}

	// note: discovery does not wait for a sibling provider's relay contract to
	// reach node1 (the route_not_found race), so the calls wait until node1
	// names each provider's node as its host.
	admin1 := connect(n1.Endpoint, n1.Token)
	nearHosts, err1 := awaitHost(ctx, admin1, near.id, mustParse(n1.Identity))
	farHosts, err2 := awaitHost(ctx, admin1, far.id, mustParse(n2.Identity))
	known := err1 == nil && err2 == nil && len(nearHosts) == 1 && len(farHosts) == 1
	if err := step(facts, "node1_knows_both_hosts", known, "apphost.hosts on node1 named %v (err %v) and %v (err %v); want node1 and node2", nearHosts, err1, farHosts, err2); err != nil {
		return err
	}

	// the consumer calls each discovered provider with a message of its own
	msgs := map[string]string{near.id.String(): "hello near", far.id.String(): "hello far"}
	echoed := true
	for _, p := range d.providers {
		answer, err := call(ctx, consumer, p, msgs[p.String()])
		fmt.Fprintf(os.Stderr, "echo on %v answered %q, err %v\n", p, answer, err)
		echoed = echoed && err == nil && answer == msgs[p.String()]
	}
	if err := step(facts, "each_provider_echoed", echoed, "a call was not echoed"); err != nil {
		return err
	}
	own := near.answeredOnly(consumer.id, msgs[near.id.String()]) && far.answeredOnly(consumer.id, msgs[far.id.String()])
	return step(facts, "each_provider_answered_its_own_call", own, "node1's provider answered %v, node2's %v; want one call each, from %v", near.answered(), far.answered(), consumer.id)
}

func startProvider(ctx *astral.Context, n node) (*echoApp, error) {
	a, err := register(ctx, n, serveApps)
	if err != nil {
		return nil, err
	}
	return startEcho(ctx, a, service)
}

func holdsBoth(list []*astral.Identity, a, b *astral.Identity) bool {
	var hasA, hasB bool
	for _, id := range list {
		hasA = hasA || id.IsEqual(a)
		hasB = hasB || id.IsEqual(b)
	}
	return hasA && hasB
}

func mustParse(s string) *astral.Identity {
	id, err := astral.ParseIdentity(s)
	if err != nil {
		panic(err)
	}
	return id
}
