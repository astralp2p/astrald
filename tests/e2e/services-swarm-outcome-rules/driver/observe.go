package main

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
)

// observation is everything one run saw. verify.py judges it.
type observation struct {
	Provider string `json:"provider"` // node2's provider app
	Consumer string `json:"consumer"` // the app on node1 that discovers across the swarm

	// plain discovery of an open service, sent to node2 over a link
	MemberPlain   outcome `json:"member_plain"`
	StrangerPlain outcome `json:"stranger_plain"`
	// a discovery in an app's name, the form node1 sends node2 when it carries
	// a swarm discovery, naming the User
	MemberForUser   outcome `json:"member_for_user"`
	StrangerForUser outcome `json:"stranger_for_user"`
	// a stranger asking node1 to carry its discovery to the swarm
	StrangerSwarm outcome `json:"stranger_swarm"`
	// an app on node1 discovering across the swarm
	SwarmAnswered outcome `json:"swarm_answered"`
	SwarmSilent   outcome `json:"swarm_silent"` // node2 never concludes inside node1's budget
	SwarmLost     outcome `json:"swarm_lost"`   // node2 reports a service incomplete
}

// observe advertises one answered and one silent service on node2, then
// records how each asker's discovery came back. It closes both bindings
// before it returns, so the next run can advertise the same names.
func observe(w *world) (*observation, error) {
	ctx := astral.NewContext(nil)
	consumer, f, err := app(w.admin1, w.n1)
	if err != nil {
		return nil, err
	}
	provider, c2, err := app(w.admin2, w.n2, serveApps)
	if err != nil {
		return nil, err
	}
	if err := w.grantDiscovery(consumer, open, slow, gone); err != nil {
		return nil, err
	}

	answer := func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
		return &services.Update{Available: true}, nil
	}
	bOpen, err := c2.Advertise(ctx, []string{open}, answer)
	if err != nil {
		return nil, fmt.Errorf("advertise %s on node2 (is a binding of an earlier run still open?): %w", open, err)
	}
	defer bOpen.Close()
	// why: a handler that blocks never answers, so node2's evaluation of the
	// service is the one initial work that cannot resolve.
	block := make(chan struct{})
	defer close(block)
	bSlow, err := c2.Advertise(ctx, []string{slow}, func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
		<-block
		return nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("advertise %s on node2: %w", slow, err)
	}
	defer bSlow.Close()

	// why: this provider is asked, then leaves with the ask unanswered. node2
	// then reports the service incomplete well inside node1's budget, which
	// is a different path from node1 giving up on a member.
	asked := make(chan struct{}, 1)
	bGone, err := c2.Advertise(ctx, []string{gone}, func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
		select {
		case asked <- struct{}{}:
		default:
		}
		<-block
		return nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("advertise %s on node2: %w", gone, err)
	}
	defer bGone.Close()
	go func() {
		select {
		case <-asked:
			_ = bGone.Close()
		case <-time.After(deadline):
		}
	}()

	// why the askers carry their node's token and name node2 as the target:
	// node2 sees a query over a link from that node, as node-exposure.md
	// describes for a linked peer.
	member := servicescli.New(w.node2, connect(w.n1))
	stranger := servicescli.New(w.node2, connect(w.s))
	strangerAtNode1 := servicescli.New(w.node1, connect(w.s))

	names := []string{open}
	obs := &observation{Provider: provider.String(), Consumer: consumer.String()}
	obs.MemberPlain = discover(member, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.Discover(ctx, names, false)
	})
	obs.StrangerPlain = discover(stranger, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.Discover(ctx, names, false)
	})
	obs.MemberForUser = discover(member, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverForTagged(ctx, w.user, names, false)
	})
	obs.StrangerForUser = discover(stranger, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverForTagged(ctx, w.user, names, false)
	})
	obs.StrangerSwarm = discover(strangerAtNode1, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverIn(ctx, services.ReachSwarm, names, false)
	})
	obs.SwarmAnswered = discover(f, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverIn(ctx, services.ReachSwarm, names, false)
	})
	obs.SwarmSilent = discover(f, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverIn(ctx, services.ReachSwarm, []string{open, slow}, false)
	})
	obs.SwarmLost = discover(f, func(ctx *astral.Context, c *servicescli.Client) (<-chan servicescli.Event, error) {
		return c.DiscoverIn(ctx, services.ReachSwarm, []string{open, gone}, false)
	})
	return obs, nil
}
