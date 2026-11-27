package nodes

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// rowsResolver yields the rows listed for each identity.
type rowsResolver struct {
	rows sig.Map[string, []*nodes.EndpointWithTTL]
}

func (r *rowsResolver) ResolveEndpoints(_ *astral.Context, id *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	rows, _ := r.rows.Get(id.String())
	return sig.ArrayToChan(rows), nil
}

func (r *rowsResolver) set(id *astral.Identity, endpoints ...exonetapi.Endpoint) {
	rows := make([]*nodes.EndpointWithTTL, len(endpoints))
	for i, e := range endpoints {
		rows[i] = nodes.NewEndpointWithTTL(e)
	}
	r.rows.Set(id.String(), rows)
}

// endpointConn is a dialed conn that reports the endpoint it was dialed at, as a transport's conn does.
type endpointConn struct {
	exonet.Conn
	remote exonetapi.Endpoint
}

func (c endpointConn) RemoteEndpoint() exonetapi.Endpoint { return c.remote }

// autoLinkEnd is a link end that links the way the automatic callers do: its
// basic strategy resolves rows and dials them through the stub exonet.
type autoLinkEnd struct {
	*linkEnd
	rows   *rowsResolver
	exonet *stubExonet
}

func newAutoLinkEnd(t *testing.T, budgets map[string]time.Duration) *autoLinkEnd {
	t.Helper()
	end := &autoLinkEnd{linkEnd: newLinkEnd(t), rows: &rowsResolver{}, exonet: &stubExonet{}}
	end.mod.Exonet = end.exonet
	end.mod.resolvers.Add(end.rows)
	end.mod.RegisterLinkStrategy(nodesmod.StrategyBasic, &BasicLinkStrategyFactory{
		mod:        end.mod,
		networks:   basicNetworks,
		rowBudgets: budgets,
	})
	return end
}

// ensureLink runs the task MaintainLinkTask schedules (mod/nodes/src/maintain_link_task.go):
// strategies basic and tor, any network, no new link forced. It returns the task and how long it ran.
func (end *autoLinkEnd) ensureLink(target *astral.Identity, limit time.Duration) (*EnsureLinkTask, time.Duration) {
	task := end.mod.NewEnsureLinkTask(target, []string{nodesmod.StrategyBasic, nodesmod.StrategyTor}, nil, false).(*EnsureLinkTask)
	ctx, cancel := astral.NewContext(context.Background()).WithTimeout(limit)
	defer cancel()
	start := time.Now()
	task.Run(ctx)
	return task, time.Since(start)
}

// stallDial blocks until ctx ends, as a dial to a black-holed row does.
func stallDial(ctx *astral.Context) (exonet.Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Minute):
		return nil, errors.New("dial outlived the test")
	}
}

// TestAutomaticLinkingRelinksAKCPOnlyPeer: a restarted node, with no link
// left, whose peer answers only on its kcp row (tcp blocked) links to it over
// kcp through the task the automatic callers schedule, within the 5 s kcp
// budget, and the link reports network kcp.
func TestAutomaticLinkingRelinksAKCPOnlyPeer(t *testing.T) {
	peer := newLinkEnd(t)
	restarted := newAutoLinkEnd(t, rowBudgetsOf(basicNetworks))
	restarted.rows.set(peer.id, mustTCP(t, "192.0.2.2:1791"), mustKCP(t, "192.0.2.2:1792"))
	restarted.exonet.dial = func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		if ep.Network() != "kcp" {
			return nil, errors.New("connection refused")
		}
		return endpointConn{peer.pipeTo(t), ep}, nil
	}

	task, took := restarted.ensureLink(peer.id, effectTimeout)
	if task.Err != nil {
		t.Fatalf("the automatic link task failed: %v", task.Err)
	}
	if took > transportProfiles["kcp"].RowBudget {
		t.Fatalf("the kcp link came up after %v; want within %v", took, transportProfiles["kcp"].RowBudget)
	}
	if task.Info.Network != "kcp" {
		t.Fatalf("the link reports network %v; want kcp", task.Info.Network)
	}
	if link := restarted.mod.linkPool.SelectLinkWith(peer.id); link == nil || link.Network() != "kcp" {
		t.Fatal("the pool holds no kcp link to the peer")
	}
	t.Logf("kcp link up after %v", took)
}

// TestAutomaticLinkingTakesLiveTCPBesideDeadKCPRow: a dead kcp row listed
// before a live tcp row, at the default 5 s kcp budget, does not hold the
// link back: it comes up over tcp in under 2 s.
func TestAutomaticLinkingTakesLiveTCPBesideDeadKCPRow(t *testing.T) {
	peer := newLinkEnd(t)
	node := newAutoLinkEnd(t, rowBudgetsOf(basicNetworks))
	node.rows.set(peer.id, mustKCP(t, "192.0.2.2:1792"), mustTCP(t, "192.0.2.2:1791"))
	node.exonet.dial = func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		if ep.Network() == "kcp" {
			return silentPeer(t), nil
		}
		return endpointConn{peer.pipeTo(t), ep}, nil
	}

	task, took := node.ensureLink(peer.id, effectTimeout)
	if task.Err != nil {
		t.Fatalf("the automatic link task failed: %v", task.Err)
	}
	if took >= 2*time.Second {
		t.Fatalf("the tcp link came up after %v; want under 2 s", took)
	}
	if task.Info.Network != "tcp" {
		t.Fatalf("the link reports network %v; want tcp", task.Info.Network)
	}
	t.Logf("tcp link up after %v beside the dead kcp row", took)
}

// TestAutomaticLinkingLinksAGatewayOnlyPeer: a peer whose only row is a gw
// row through gateway G links over gw through the task the automatic callers
// schedule, with no nodes.new_link.
// note: the stub dial stands in for a gw dial that G accepts; G's routing and
// its use-gateway grant belong to mod/gateway.
func TestAutomaticLinkingLinksAGatewayOnlyPeer(t *testing.T) {
	peer := newLinkEnd(t)
	node := newAutoLinkEnd(t, rowBudgetsOf(basicNetworks))
	node.rows.set(peer.id, gateway.NewEndpoint(newTestIdentity(t), peer.id))
	node.exonet.dial = func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		if ep.Network() != "gw" {
			return nil, errors.New("unreachable")
		}
		return endpointConn{peer.pipeTo(t), ep}, nil
	}

	task, _ := node.ensureLink(peer.id, effectTimeout)
	if task.Err != nil {
		t.Fatalf("the automatic link task failed: %v", task.Err)
	}
	if task.Info.Network != "gw" {
		t.Fatalf("the link reports network %v; want gw", task.Info.Network)
	}
}

// TestAutomaticLinkingMeasuresTenDeadRowsBesideTCP: 6 dead kcp rows and 4
// stalled gw rows listed before one live tcp row, at the default budgets,
// hold all 8 workers until the first kcp budget frees one; the link then
// comes up over tcp, about 5 s after the round starts.
func TestAutomaticLinkingMeasuresTenDeadRowsBesideTCP(t *testing.T) {
	peer := newLinkEnd(t)
	node := newAutoLinkEnd(t, rowBudgetsOf(basicNetworks))
	var rows []exonetapi.Endpoint
	for range 6 {
		rows = append(rows, mustKCP(t, "192.0.2.3:1792"))
	}
	for range 4 {
		rows = append(rows, gateway.NewEndpoint(newTestIdentity(t), peer.id))
	}
	node.rows.set(peer.id, append(rows, mustTCP(t, "192.0.2.2:1791"))...)
	node.exonet.dial = func(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		switch ep.Network() {
		case "kcp":
			return silentPeer(t), nil
		case "gw":
			return stallDial(ctx)
		}
		return endpointConn{peer.pipeTo(t), ep}, nil
	}

	task, took := node.ensureLink(peer.id, 30*time.Second)
	if task.Err != nil {
		t.Fatalf("the automatic link task failed: %v", task.Err)
	}
	if task.Info.Network != "tcp" {
		t.Fatalf("the link reports network %v; want tcp", task.Info.Network)
	}
	if limit := transportProfiles["kcp"].RowBudget + time.Second; took > limit {
		t.Fatalf("the tcp link came up after %v; want within %v, the kcp budget", took, limit)
	}
	t.Logf("tcp link up after %v behind 10 dead rows", took)
}

// TestAutomaticLinkingEndsAGatewayCycleWithinTheRowBudget: the target's only
// row is a gw row through G1, G1's only row a gw row through G2, and G2's
// only row a gw row through G1. Each gw dial first asks the pool for a link
// to its gateway, as the gw dialer's routed query to the gateway does. The
// round ends without a link within the gw budget, well inside the caller's
// 60 s, and no dial follows it.
func TestAutomaticLinkingEndsAGatewayCycleWithinTheRowBudget(t *testing.T) {
	const gwBudget = 300 * time.Millisecond
	node := newAutoLinkEnd(t, map[string]time.Duration{"kcp": testRowBudget, "gw": gwBudget})
	target, g1, g2 := newTestIdentity(t), newTestIdentity(t), newTestIdentity(t)
	node.rows.set(target, gateway.NewEndpoint(g1, target))
	node.rows.set(g1, gateway.NewEndpoint(g2, g1))
	node.rows.set(g2, gateway.NewEndpoint(g1, g2))

	var dials atomic.Uint64
	node.exonet.dial = func(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		dials.Add(1)
		gw := ep.(*gateway.Endpoint)
		res := <-node.mod.linkPool.RetrieveLink(ctx, gw.GatewayID, WithStrategies(nodesmod.StrategyBasic, nodesmod.StrategyTor))
		if res.Err != nil {
			return nil, res.Err
		}
		return nil, errors.New("the gateway refused the connection")
	}

	task, took := node.ensureLink(target, 60*time.Second)
	if task.Err == nil {
		t.Fatal("the automatic link task linked through a gateway cycle")
	}
	if limit := gwBudget + time.Second; took > limit {
		t.Fatalf("the round through the gateway cycle ended after %v; want within %v, the gw budget", took, limit)
	}
	n := dials.Load()
	time.Sleep(absenceWindow)
	if after := dials.Load(); after != n || n != 3 {
		t.Fatalf("gw dials: %v at the round's end, %v after; want 3, one per row, and none after", n, after)
	}
	t.Logf("the round ended after %v with %v; %v gw dials", took, task.Err, n)
}
