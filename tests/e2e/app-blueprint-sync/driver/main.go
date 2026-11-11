// Command driver tells one story across node1 and node2: an app on node2 with
// types of its own syncs its blueprints, advertises an offering whose info
// uses one of them, and answers a query with an object of the other; a
// consumer app on node1 discovers it and decodes both.
//
//	driver drive   <session.json>   consumer; starts the provider; prints facts as JSON
//	driver provide <session.json>   provider; runs until its stdin closes
//
// The provider is a process of its own: the types live in the process-wide
// registry, and the consumer must be a process that never had them.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"time"

	objectsClient "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const (
	service = "app-blueprint-sync-offer"
	opName  = "app-blueprint-sync-reading"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|provide <session.json>")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "provide":
		err = provide(os.Args[2])
	case "drive":
		facts := map[string]any{}
		err = drive(os.Args[2], facts)
		out, _ := json.Marshal(map[string]any{"app_blueprint_sync": facts})
		fmt.Println(string(out))
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// startProvider runs the provider as a child process and returns once it has
// advertised, with the facts it printed.
func startProvider(session string) (stop func(), facts map[string]any, err error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(exe, "provide", session)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	stop = func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	select {
	case l, ok := <-lines:
		if !ok {
			stop()
			return nil, nil, fmt.Errorf("provider exited before it advertised")
		}
		if err := json.Unmarshal([]byte(l), &facts); err != nil {
			stop()
			return nil, nil, fmt.Errorf("provider printed %q: %v", l, err)
		}
		return stop, facts, nil
	case <-time.After(90 * time.Second):
		stop()
		return nil, nil, fmt.Errorf("provider not advertised within 90s")
	}
}

func drive(path string, facts map[string]any) error {
	nodes, userToken, err := loadSession(path)
	if err != nil {
		return err
	}
	n1, n2 := nodes["node1"], nodes["node2"]
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)

	stop, pf, err := startProvider(path)
	if err != nil {
		return err
	}
	defer stop()
	for k, v := range pf {
		facts[k] = v
	}
	provider, err := astral.ParseIdentity(pf["provider_identity"].(string))
	if err != nil {
		return err
	}

	consumer, err := register(ctx, n1)
	if err != nil {
		return err
	}

	// note: discovery does not wait for the provider's relay contract to reach
	// node1, so nothing is called before node1 names node2 as the host.
	if _, err := awaitHost(ctx, connect(n1.Endpoint, n1.Token), provider, node2); err != nil {
		return err
	}

	user := connect(n1.Endpoint, userToken)
	onNode1 := objectsClient.New(nil, user)
	types := []string{offerType, readingType}

	// 1. Nothing is learned yet: this process does not know the reply type.
	d := discoverOnce(ctx, consumer, services.ReachSwarm, service)
	facts["discover_before_learning_found"] = len(d.providers) > 0
	_, err = callReading(ctx, consumer, provider)
	facts["reply_before_learning_error"] = fmt.Sprint(err)

	// 2. The consumer's process learns the schemas, pulled from node2 as the
	// User (an app of node1 is not permitted on node2's blueprints). node1
	// learns nothing, and the reply decodes anyway: node1 relays it as bytes.
	bps := map[string]*astral.Blueprint{}
	for _, t := range types {
		bp, err := objectsClient.New(node2, user).GetBlueprint(ctx, t)
		if err != nil {
			return flowError(facts, "get_blueprint %s from node2: %v", t, err)
		}
		bps[t] = bp
		if _, err := astral.DefaultBlueprints().RegisterBlueprint(bp); err != nil {
			return flowError(facts, "register %s in the consumer: %v", t, err)
		}
	}
	held, err := onNode1.Blueprints(ctx)
	if err != nil {
		return flowError(facts, "objects.blueprints on node1: %v", err)
	}
	facts["node1_held_types_at_reply"] = holdsAny(held, types)
	reading, err := callReading(ctx, consumer, provider)
	facts["reply_error"] = fmt.Sprint(err)
	if ro, ok := reading.(*astral.RuntimeObject); ok {
		facts["reply_type"] = ro.ObjectType()
		facts["reply_label"] = fmt.Sprint(ro.Get("Label"))
		facts["reply_serial"] = fmt.Sprint(ro.Get("Serial"))
	}

	// 3. Discovery relays the offering info through node1, which decodes it:
	// the schemas must be on node1 too.
	d = discoverOnce(ctx, consumer, services.ReachSwarm, service)
	facts["discover_with_types_in_process_only_found"] = len(d.providers) > 0
	for _, t := range types {
		if _, err := onNode1.Register(ctx, bps[t]); err != nil && !errors.Is(err, astral.ErrAlreadyRegistered) {
			return flowError(facts, "register %s on node1: %v", t, err)
		}
	}
	d = discoverOnce(ctx, consumer, services.ReachSwarm, service)
	facts["found_the_provider"] = d.complete() && len(d.providers) == 1 && d.providers[0].IsEqual(provider)
	if len(d.updates) == 1 && d.updates[0].Info != nil {
		for _, o := range d.updates[0].Info.Objects() {
			if ro, ok := o.(*astral.RuntimeObject); ok {
				facts["info_type"] = ro.ObjectType()
				facts["info_label"] = fmt.Sprint(ro.Get("Label"))
				facts["info_tier"] = fmt.Sprint(ro.Get("Tier"))
			}
		}
	}
	return nil
}

// flowError records a failure of the flow itself as a fact and ends the drive
// without an error: the oracle judges what the nodes did, so a node that
// refuses a step is a verdict on astrald, not a driver that could not run.
func flowError(facts map[string]any, format string, args ...any) error {
	facts["error"] = fmt.Sprintf(format, args...)
	return nil
}

// holdsAny reports whether names lists any of types.
func holdsAny(names, types []string) bool {
	for _, n := range names {
		if slices.Contains(types, n) {
			return true
		}
	}
	return false
}

// callReading queries the provider's reading op as caller and returns the
// first object it answers with.
func callReading(ctx *astral.Context, caller *app, provider *astral.Identity) (astral.Object, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(15 * time.Second)
	defer cancel()
	ch, err := caller.client.WithTarget(provider).QueryChannel(cctx, opName, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	return ch.Receive()
}
