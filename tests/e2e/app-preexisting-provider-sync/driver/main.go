// Command driver tells the consumer half and the provider half of one story:
// a provider app registered and advertising on a node that is not yet in the
// User's swarm becomes discoverable and callable once that node is adopted.
//
//	driver provider <session.json>               serves until stdin closes; prints JSON lines
//	driver consumer <session.json> <provider-id> prints facts as JSON
//
// The Python driver (script.py) runs the provider, adopts its node, then runs
// the consumer.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const service = "ppsync-echo"

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: driver provider <session.json> | consumer <session.json> <provider-id>")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "provider":
		err = runProvider(os.Args[2])
	case "consumer":
		if len(os.Args) != 4 {
			err = errors.New("consumer needs a provider id")
			break
		}
		err = runConsumer(os.Args[2], os.Args[3])
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func emit(v any) {
	out, _ := json.Marshal(v)
	fmt.Println(string(out))
}

// runProvider registers the provider app on the joiner, advertises the service,
// prints its identity, and serves until stdin closes; then it prints the
// callers it answered.
func runProvider(path string) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	a, err := register(ctx, nodes["ppsyncjoiner"], serveApps)
	if err != nil {
		return err
	}
	p, err := startEcho(ctx, a, service)
	if err != nil {
		return err
	}
	emit(map[string]string{"provider": a.id.String()})
	_, _ = io.Copy(io.Discard, bufio.NewReader(os.Stdin))
	p.stop()
	emit(map[string]any{"served": p.served()})
	return nil
}

// runConsumer registers an app on the User's node, finds the provider with
// reach=swarm and calls it.
func runConsumer(path, providerID string) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	facts := map[string]any{}
	// why exit 0 on a failed step: the oracle judges the recorded steps and
	// names the one that failed; a failure is a fact carrying its error.
	if err := consume(path, nodes, providerID, facts); err != nil {
		facts["consumer_error"] = err.Error()
		fmt.Fprintln(os.Stderr, "FAIL:", err)
	}
	emit(map[string]any{"app_preexisting_provider_sync": facts})
	return nil
}

func consume(_ string, nodes map[string]node, providerID string, facts map[string]any) error {
	user, joiner := nodes["ppsyncuser"], nodes["ppsyncjoiner"]
	provider, err := astral.ParseIdentity(providerID)
	if err != nil {
		return err
	}
	host, err := astral.ParseIdentity(joiner.Identity)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)

	consumer, err := register(ctx, user)
	if err != nil {
		return err
	}
	if err := grantDiscovery(ctx, consumer, service); err != nil {
		return err
	}
	facts["consumer"] = consumer.id.String()

	// why a window: the link to the adopted node and its first discovery
	// answer arrive after user.adopt returns.
	var d discovery
	deadline := time.Now().Add(30 * time.Second)
	for {
		d = discoverOnce(ctx, consumer, services.ReachSwarm, service)
		if (d.complete() && len(d.providers) > 0) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	found := d.complete() && len(d.providers) == 1 && d.providers[0].IsEqual(provider)
	if err := step(facts, "discovered_after_adoption", found, "swarm discovery: providers %v, outcome %+v, err %v; want %v alone", d.providers, d.outcome, d.err, provider); err != nil {
		return err
	}

	// note: the first call after discovery can fail with route_not_found (the
	// provider's relay contract may trail the discovery update). The failure is
	// a fact, not a verdict; the call is retried once, after apphost.hosts on
	// the User's node names the joiner.
	answer, err := call(ctx, consumer, provider, "hello")
	facts["first_call_route_not_found"] = false
	if err != nil && routeNotFound(err) {
		facts["first_call_route_not_found"] = true
		facts["first_call_error"] = err.Error()
		if _, herr := awaitHost(ctx, connect(user.Endpoint, user.Token), provider, host); herr != nil {
			return herr
		}
		answer, err = call(ctx, consumer, provider, "hello")
	}
	if err := step(facts, "provider_echoed", err == nil && answer == "hello", "echo answered %q, err %v; want \"hello\"", answer, err); err != nil {
		return err
	}

	hosts, err := hostsOf(ctx, connect(user.Endpoint, user.Token), provider)
	return step(facts, "user_node_knows_the_host", err == nil && len(hosts) == 1 && hosts[0].IsEqual(host), "apphost.hosts on the User's node named %v (err %v); want the joiner %v alone", hosts, err, host)
}

func routeNotFound(err error) bool {
	return errors.Is(err, astral.NewErrRouteNotFound()) ||
		strings.Contains(strings.ToLower(err.Error()), "route not found") ||
		strings.Contains(err.Error(), "route_not_found")
}
