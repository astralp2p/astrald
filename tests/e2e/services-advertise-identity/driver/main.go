// Command driver tells who an advertised service belongs to, across node1 and
// node2: a service an app advertises on node2 is found, on both nodes, under
// the app's own identity; and nothing lets an app advertise as someone else.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/astralp2p/astral-go/astral"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts := map[string]bool{}
	err := story(os.Args[2], facts)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"services_advertise_identity": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// Every service name carries the test's prefix, so no other test in the world
// sees these providers.
const (
	svcMain      = "sai-main"       // the honest provider's service
	svcRaw       = "sai-raw"        // a hand-written binding that answers honestly
	svcSpoofApp  = "sai-spoof-app"  // answered as another app
	svcSpoofNode = "sai-spoof-node" // answered as the node
	svcSpoofName = "sai-spoof-name" // answered as another service
	svcNoGrant   = "sai-nogrant"    // advertised without ServeApps
	svcLink      = "sai-link"       // advertised over a link
	svcFresh     = "sai-fresh"      // listed beside a name that is taken
	info         = "sai-info"
)

var allNames = []string{svcMain, svcRaw, svcSpoofApp, svcSpoofNode, svcSpoofName, svcNoGrant, svcLink, svcFresh}

func story(path string, facts map[string]bool) error {
	nodes, err := loadNodes(path)
	if err != nil {
		return err
	}
	n1, n2 := nodes["node1"], nodes["node2"]
	node1, err := astral.ParseIdentity(n1.Identity)
	if err != nil {
		return err
	}
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)

	var bindings closer
	defer bindings.closeAll()

	// the consumers: one app on each node, each granted discovery of every name
	c1, err := register(ctx, n1)
	if err != nil {
		return err
	}
	c2, err := register(ctx, n2)
	if err != nil {
		return err
	}
	for _, c := range []*app{c1, c2} {
		if err := grantDiscovery(ctx, c, allNames...); err != nil {
			return err
		}
	}

	// the honest provider: an app on node2 that advertises sai-main
	p, err := register(ctx, n2)
	if err != nil {
		return err
	}
	pb, err := advertise(ctx, p, []string{svcMain})
	if err != nil {
		return fmt.Errorf("the provider could not advertise: %w", err)
	}
	bindings.add(pb.close)

	// 1. the provider is the app, on every node that shows it
	d := swarm(ctx, c1, svcMain)
	if err := step(facts, "swarm_shows_the_app", d.complete() && d.exactly(offering{svcMain, p.id}) && d.carries(info),
		"swarm discovery from node1: %v; want %v/%v alone, complete, carrying %q", d, p.id, svcMain, info); err != nil {
		return err
	}
	if err := step(facts, "provider_is_not_a_node", !p.id.IsEqual(node2) && !p.id.IsEqual(node1),
		"the app identity %v equals a node identity", p.id); err != nil {
		return err
	}
	d = local(ctx, c2, svcMain)
	if err := step(facts, "home_node_shows_the_app", d.complete() && d.exactly(offering{svcMain, p.id}),
		"local discovery on node2: %v; want %v/%v alone", d, p.id, svcMain); err != nil {
		return err
	}
	d = local(ctx, c1, svcMain)
	if err := step(facts, "other_node_has_no_local_provider", d.complete() && d.none(),
		"local discovery on node1: %v; want nothing", d); err != nil {
		return err
	}
	if err := step(facts, "provider_evaluated_the_consumer", pb.saw(c1.id) && !pb.saw(node1),
		"the provider evaluated %v; want the app %v and not node1 %v", pb.callers(), c1.id, node1); err != nil {
		return err
	}

	// 2. a hand-written binding that answers as itself is accepted: the control
	// for the spoofs below, which fail because of what they claim and not
	// because the binding is not written with the SDK
	raw, err := rawAdvertise(ctx, n2, &bindings, svcRaw, func(me *astral.Identity, service string) answer {
		return answer{name: service, provider: me}
	})
	if err != nil {
		return err
	}
	d = swarm(ctx, c1, svcRaw)
	if err := step(facts, "honest_raw_binding_is_accepted", d.complete() && d.exactly(offering{svcRaw, raw.app.id}),
		"swarm discovery of the hand-written binding: %v; want %v/%v alone", d, raw.app.id, svcRaw); err != nil {
		return err
	}

	// 3. an answer that names another provider ends the binding and shows nothing
	spoofApp, err := rawAdvertise(ctx, n2, &bindings, svcSpoofApp, func(_ *astral.Identity, service string) answer {
		return answer{name: service, provider: p.id}
	})
	if err != nil {
		return err
	}
	spoofNode, err := rawAdvertise(ctx, n2, &bindings, svcSpoofNode, func(_ *astral.Identity, service string) answer {
		return answer{name: service, provider: node2}
	})
	if err != nil {
		return err
	}
	d = swarm(ctx, c1, svcSpoofApp, svcSpoofNode)
	if err := step(facts, "answer_as_another_provider_shows_nothing", d.none() && !d.hasErr(),
		"swarm discovery of the spoofed services: %v; want no offering", d); err != nil {
		return err
	}
	if err := step(facts, "answer_as_another_provider_ends_the_binding", spoofApp.ended() && spoofNode.ended(),
		"the node left the binding that answered as another app open: %v, as the node: %v", !spoofApp.ended(), !spoofNode.ended()); err != nil {
		return err
	}
	d = local(ctx, c2, svcSpoofApp, svcSpoofNode)
	if err := step(facts, "answer_as_another_provider_shows_nothing_at_home", d.none(),
		"local discovery on node2 of the spoofed services: %v; want no offering", d); err != nil {
		return err
	}

	// 4. an answer for another service ends the binding and shows nothing
	spoofName, err := rawAdvertise(ctx, n2, &bindings, svcSpoofName, func(_ *astral.Identity, service string) answer {
		return answer{name: svcMain, provider: nil}
	})
	if err != nil {
		return err
	}
	d = swarm(ctx, c1, svcSpoofName, svcMain)
	if err := step(facts, "answer_for_another_service_shows_nothing", d.exactly(offering{svcMain, p.id}),
		"swarm discovery of %v and %v: %v; want %v/%v alone", svcSpoofName, svcMain, d, p.id, svcMain); err != nil {
		return err
	}
	if err := step(facts, "answer_for_another_service_ends_the_binding", spoofName.ended(),
		"the node left the binding that answered for %v open", svcMain); err != nil {
		return err
	}

	// 5. without ServeApps an app cannot advertise at all
	stray, err := register(ctx, n2)
	if err != nil {
		return err
	}
	if err := revokeServeApps(ctx, stray); err != nil {
		return fmt.Errorf("revoking serve_apps from the stray app: %w", err)
	}
	sb, err := advertise(ctx, stray, []string{svcNoGrant})
	if sb != nil {
		bindings.add(sb.close)
	}
	if err := step(facts, "no_serve_apps_is_refused", isNotPermitted(err), "advertising without serve_apps: err %v; want %q", err, notPermitted); err != nil {
		return err
	}
	d = swarm(ctx, c1, svcNoGrant)
	if err := step(facts, "no_serve_apps_shows_nothing", d.complete() && d.none(), "swarm discovery of %v: %v; want nothing", svcNoGrant, d); err != nil {
		return err
	}

	// 6. over a link nobody advertises on another node: an app of node1 is
	// turned away on its way, and node1 itself, which reaches the op, is told
	// the op does not admit a network origin
	a1, err := register(ctx, n1)
	if err != nil {
		return err
	}
	lb, err := advertiseOn(ctx, a1, node2, []string{svcLink})
	if lb != nil {
		bindings.add(lb.close)
	}
	if err := step(facts, "advertise_over_a_link_is_refused", err != nil && lb == nil, "an app advertising on node2 from node1: err %v; want a refusal", err); err != nil {
		return err
	}
	sibling := &app{id: node1, client: connect(n1.Endpoint, n1.Token), n: n1}
	lb, err = advertiseOn(ctx, sibling, node2, []string{svcLink})
	if lb != nil {
		bindings.add(lb.close)
	}
	if err := step(facts, "advertise_over_a_link_hits_the_origin_guard", err != nil && err.Error() == astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(),
		"node1 advertising on node2: err %v; want %q", err, astral.NewErrOriginNotAllowed(astral.OriginNetwork)); err != nil {
		return err
	}
	d = swarm(ctx, c1, svcLink)
	d2 := local(ctx, c2, svcLink)
	if err := step(facts, "advertise_over_a_link_shows_nothing", d.none() && d2.none(),
		"discovery of %v: swarm %v, on node2 %v; want nothing", svcLink, d, d2); err != nil {
		return err
	}

	// 7. a name in use is refused whole, and belongs to its provider alone
	_, err = advertise(ctx, p, []string{svcMain})
	if err := step(facts, "taken_name_is_refused", isAdvertised(err), "advertising %v twice: err %v; want %q", svcMain, err, advertised); err != nil {
		return err
	}
	_, err = advertise(ctx, p, []string{svcFresh, svcMain})
	if err := step(facts, "taken_name_refuses_the_whole_set", isAdvertised(err), "advertising %v beside a taken name: err %v; want %q", svcFresh, err, advertised); err != nil {
		return err
	}
	d = swarm(ctx, c1, svcFresh, svcMain)
	if err := step(facts, "refused_set_shows_nothing", d.exactly(offering{svcMain, p.id}),
		"swarm discovery of %v and %v: %v; want %v/%v alone", svcFresh, svcMain, d, p.id, svcMain); err != nil {
		return err
	}

	// the same name from another app, on either node, is another provider
	q, err := register(ctx, n1)
	if err != nil {
		return err
	}
	qb, err := advertise(ctx, q, []string{svcMain})
	if err != nil {
		return fmt.Errorf("another app could not advertise %v: %w", svcMain, err)
	}
	bindings.add(qb.close)
	d = swarm(ctx, c1, svcMain)
	if err := step(facts, "same_name_from_another_app_is_another_provider", d.complete() && d.exactly(offering{svcMain, p.id}, offering{svcMain, q.id}),
		"swarm discovery of %v: %v; want %v and %v, once each", svcMain, d, p.id, q.id); err != nil {
		return err
	}
	d = local(ctx, c2, svcMain)
	if err := step(facts, "home_node_does_not_show_the_other_nodes_provider", d.exactly(offering{svcMain, p.id}),
		"local discovery on node2: %v; want %v alone", d, p.id); err != nil {
		return err
	}

	// 8. closing a binding takes its provider away, and only its own
	qb.close()
	pb.close()
	raw.close()
	if err := step(facts, "closed_bindings_leave_discovery", waitNone(ctx, c1, svcMain, svcRaw), "swarm discovery still shows a provider after its binding closed"); err != nil {
		return err
	}
	return nil
}
