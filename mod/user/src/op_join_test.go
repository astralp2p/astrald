package user

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	dirmod "github.com/astralp2p/astrald/mod/dir"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// swarmNode is the unclaimed node user.join runs on. Every query it routes
// reaches the swarm node, which answers it with answer, while routes holds a
// route to the swarm node; without one the query fails with route not found.
// The node records each routed query, the zone it was routed in and when.
type swarmNode struct {
	astral.Node
	id     *astral.Identity
	answer astral.Object
	routes *joinNodes
	routed chan routedQuery
}

type routedQuery struct {
	query *astral.Query
	zone  astral.Zone
	at    time.Time
}

func (n *swarmNode) Identity() *astral.Identity { return n.id }

func (n *swarmNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.routed <- routedQuery{query: q.Query, zone: ctx.Zone(), at: time.Now()}
	if !n.routes.hasRoute(q.Target) {
		return query.RouteNotFound()
	}
	op, err := routing.NewOp(func(_ *astral.Context, q *routing.IncomingQuery) error {
		ch := q.Accept()
		// why: the joiner sends its JoinRequest first, and waits on the send.
		_, _ = ch.Receive()
		return ch.Send(n.answer)
	})
	if err != nil {
		return nil, err
	}
	return op.RouteQuery(ctx, q, w)
}

// namedDir resolves the names it holds and refuses every other name.
type namedDir struct {
	dirmod.Module
	names map[string]*astral.Identity
}

func (d *namedDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, ok := d.names[name]; ok {
		return id, nil
	}
	return nil, errors.New("unknown name")
}

// joinNodes is the nodes module of the joining node: the swarm node resolves
// one endpoint from routeAt on, and nothing while routeAt is nil. No node is
// linked.
type joinNodes struct {
	nodesmod.Module
	target  *astral.Identity
	routeAt atomic.Pointer[time.Time]
}

func (n *joinNodes) IsLinked(*astral.Identity) bool { return false }

func (n *joinNodes) ResolveEndpoints(_ *astral.Context, id *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	if !n.hasRoute(id) {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}
	ep := &tcp.Endpoint{IP: ip.IP{192, 168, 1, 20}, Port: 1791}
	return sig.ArrayToChan([]*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(ep, time.Minute)}), nil
}

// hasRoute reports whether id is the swarm node and resolves an endpoint now.
func (n *joinNodes) hasRoute(id *astral.Identity) bool {
	at := n.routeAt.Load()
	return id.IsEqual(n.target) && at != nil && !time.Now().Before(*at)
}

// joinNearby is the nearby module of the joining node. A scan hands the
// swarm node's stealth status to the user module's recognizer, as nearby
// does, and, when the swarm node answers (replyAfter >= 0), makes it resolve
// replyAfter later.
type joinNearby struct {
	nearbymod.Module
	mod        *Module
	routes     *joinNodes
	status     *nearby.StatusMessage
	replyAfter time.Duration

	scans      atomic.Uint64
	recognized atomic.Pointer[nearbymod.Recognition]
}

func (n *joinNearby) Scan() error {
	n.scans.Add(1)
	n.recognized.Store(n.mod.RecognizeStatus(n.status))
	if n.replyAfter >= 0 {
		at := time.Now().Add(n.replyAfter)
		n.routes.routeAt.Store(&at)
	}
	return nil
}

type joinFixture struct {
	mod       *Module
	node      *swarmNode
	routes    *joinNodes
	nearby    *joinNearby
	swarmID   *astral.Identity
	swarmUser *astral.Identity
}

// newJoinFixture is an unclaimed node that names its swarm node "swarm", the
// zero identity "anyone" and itself "self". The swarm node resolves from the
// start; a scan answers with its stealth status at once.
func newJoinFixture(t *testing.T, answer astral.Object) *joinFixture {
	t.Helper()
	f := &joinFixture{swarmID: astral.GenerateIdentity(), swarmUser: astral.GenerateIdentity()}
	f.routes = &joinNodes{target: f.swarmID}
	start := time.Time{}
	f.routes.routeAt.Store(&start)
	keys := newJoinKeyring()
	f.node = &swarmNode{id: keys.newKey(), answer: answer, routes: f.routes, routed: make(chan routedQuery, 1)}
	f.mod = &Module{
		Deps: Deps{Dir: &namedDir{names: map[string]*astral.Identity{
			"swarm":  f.swarmID,
			"anyone": astral.Anyone,
			"self":   f.node.id,
		}}, Apphost: &tokenIssuer{state: user.StateUnclaimed}, Sys: &tokenIssuer{state: user.StateUnclaimed},
			Nodes: f.routes},
		node:   f.node,
		log:    log.New(f.node.id),
		joiner: newSwarmJoiner(f.node.id, keys, memJoinKeeper{}, nil),
	}
	f.nearby = &joinNearby{mod: f.mod, routes: f.routes, status: statusOf(t, stealthHint(t, f.swarmID, f.swarmUser))}
	f.mod.Nearby = f.nearby
	return f
}

// unroute makes the swarm node resolve nothing until a scan answers
// replyAfter later; a negative replyAfter never answers.
func (f *joinFixture) unroute(replyAfter time.Duration) {
	f.routes.routeAt.Store(nil)
	f.nearby.replyAfter = replyAfter
}

// routeJoin sends one user.join query from caller with origin, in a context
// without the Network zone, as a session without a token has. The requester
// stays connected until the op returns. A rejected query answers the
// rejection.
func routeJoin(t *testing.T, f *joinFixture, caller *astral.Identity, origin, queryString string) ([]astral.Object, error) {
	t.Helper()

	op, err := routing.NewOp(f.mod.OpJoin)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).ExcludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(caller, f.node.id, queryString, nil))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	w := &answerWriter{}
	requester, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		return nil, err
	}
	defer requester.Close()

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}
	return w.objects(t), nil
}

// TestJoinCarriesTheRequestAsTheNode: user.join sends its JoinRequest to the
// named node's user.carry_join, as this node and with the Network zone, and
// answers the end of the join the carrier sends.
func TestJoinCarriesTheRequestAsTheNode(t *testing.T) {
	f := newJoinFixture(t, astral.Err(errJoinDeclined))

	objs, err := routeJoin(t, f, f.node.id, astral.OriginLocal, "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	routed := <-f.node.routed
	if !routed.query.Target.IsEqual(f.swarmID) {
		t.Fatalf("request sent to %v; want the swarm node %v", routed.query.Target, f.swarmID)
	}
	if !routed.query.Caller.IsEqual(f.node.id) {
		t.Fatalf("request sent as %v; want the node %v", routed.query.Caller, f.node.id)
	}
	if op, _ := query.Parse(routed.query.QueryString.String()); op != "user.carry_join" {
		t.Fatalf("request %q; want user.carry_join", routed.query.QueryString)
	}
	if !routed.zone.Is(astral.ZoneNetwork) {
		t.Fatalf("request routed in zone %v; want the Network zone", routed.zone)
	}

	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want the carrier's end of the join", objs)
	}
	if got := objs[0].(astral.Error).Error(); got != errJoinDeclined.Error() {
		t.Fatalf("error %q; want %q", got, errJoinDeclined)
	}
}

// TestJoinAnswersTheSwarmNodesRefusal: a request the swarm node declines is
// answered with the swarm node's error.
func TestJoinAnswersTheSwarmNodesRefusal(t *testing.T) {
	f := newJoinFixture(t, user.ErrRequestDeclined)

	objs, err := routeJoin(t, f, f.node.id, "", "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want one error_message", objs)
	}
	if got := objs[0].(astral.Error).Error(); !strings.Contains(got, "request declined") {
		t.Fatalf("error %q; want the swarm node's refusal", got)
	}
}

// TestJoinRejects: every refused query is answered with a single
// error_message, and no request leaves the node.
func TestJoinRejects(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		caller func(f *joinFixture) *astral.Identity
		node   string
		answer astral.Error
	}{
		{"network origin", astral.OriginNetwork, func(f *joinFixture) *astral.Identity { return f.node.id }, "swarm", astral.NewErrOriginNotAllowed(astral.OriginNetwork)},
		{"mcp origin", astral.OriginMCP, func(f *joinFixture) *astral.Identity { return f.node.id }, "swarm", astral.NewErrOriginNotAllowed(astral.OriginMCP)},
		{"caller other than the node", "", func(*joinFixture) *astral.Identity { return astral.GenerateIdentity() }, "swarm", auth.ErrNotPermitted},
		{"unknown node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "nobody", dir.ErrUnknownIdentity},
		{"anonymous node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "anyone", astral.ErrZeroIdentity},
		{"this node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "self", user.ErrJoinSelf},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newJoinFixture(t, &auth.SignedContract{})

			objs, err := routeJoin(t, f, c.caller(f), c.origin, "user.join?node="+c.node)
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != c.answer.Error() {
				t.Fatalf("the op answered %v; want %q", objs, c.answer)
			}
			select {
			case routed := <-f.node.routed:
				t.Fatalf("the node sent %v", routed.query.QueryString)
			default:
			}
		})
	}
}

// TestJoinRefusesAClaimedNode: a claimed node answers user.ErrAlreadyClaimed,
// and no request leaves the node.
func TestJoinRefusesAClaimedNode(t *testing.T) {
	f := newJoinFixture(t, &auth.SignedContract{})
	err := f.mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: astral.GenerateIdentity()}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	objs, err := routeJoin(t, f, f.node.id, "", "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want one error_message", objs)
	}
	if got, want := objs[0].(astral.Error).Error(), user.ErrAlreadyClaimed.Error(); got != want {
		t.Fatalf("error %q; want %q", got, want)
	}
	select {
	case routed := <-f.node.routed:
		t.Fatalf("the node sent %v", routed.query.QueryString)
	default:
	}
}

// TestJoinAdmitsTheManager: the appointed manager asks with its App identity,
// and the request still goes to the swarm node as this node.
func TestJoinAdmitsTheManager(t *testing.T) {
	f := newJoinFixture(t, astral.Err(errJoinDeclined))
	manager := astral.GenerateIdentity()
	f.mod.Apphost = &tokenIssuer{state: user.StateUnclaimed, manager: manager}
	f.mod.Sys = f.mod.Apphost.(*tokenIssuer)

	objs, err := routeJoin(t, f, manager, astral.OriginLocal, "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	routed := <-f.node.routed
	if !routed.query.Caller.IsEqual(f.node.id) {
		t.Fatalf("request sent as %v; want the node %v", routed.query.Caller, f.node.id)
	}
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != errJoinDeclined.Error() {
		t.Fatalf("the op answered %v; want the carrier's end of the join", objs)
	}
}

// TestJoinFindsTheSwarmNodeNearby: with no route to the swarm node, user.join
// scans while the swarm node is its join target, so the swarm node's stealth
// hint is recognized; the request leaves once the swarm node resolves, 500 ms
// after the scan, and the target is cleared on return.
func TestJoinFindsTheSwarmNodeNearby(t *testing.T) {
	f := newJoinFixture(t, astral.Err(errJoinDeclined))
	f.unroute(500 * time.Millisecond)

	objs, err := routeJoin(t, f, f.node.id, "", "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if n := f.nearby.scans.Load(); n != 1 {
		t.Fatalf("scanned %d times; want once", n)
	}
	rec := f.nearby.recognized.Load()
	if rec == nil || !rec.NodeID.IsEqual(f.swarmID) || !rec.UserID.IsEqual(f.swarmUser) {
		t.Fatalf("during the join the swarm node's hint was recognized as %+v; want %v of %v", rec, f.swarmID, f.swarmUser)
	}
	routed := <-f.node.routed
	if at := f.routes.routeAt.Load(); at == nil || routed.at.Before(*at) {
		t.Fatalf("request sent at %v, before the swarm node resolved at %v", routed.at, at)
	}
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != errJoinDeclined.Error() {
		t.Fatalf("the op answered %v; want the carrier's end of the join", objs)
	}
	if target := f.mod.joinTarget.Load(); target != nil {
		t.Fatalf("the join target is %v after the join; want none", target)
	}
}

// TestJoinWithoutARouteFailsAfterTheWait: when no route appears, user.join
// sends the request after joinWait and answers route not found.
func TestJoinWithoutARouteFailsAfterTheWait(t *testing.T) {
	f := newJoinFixture(t, astral.Err(errJoinDeclined))
	f.unroute(-1)

	start := time.Now()
	objs, err := routeJoin(t, f, f.node.id, "", "user.join?node=swarm")
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	routed := <-f.node.routed
	if waited := routed.at.Sub(start); waited < joinWait || waited > joinWait+time.Second {
		t.Fatalf("request sent after %v; want after the %v wait", waited, joinWait)
	}
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" {
		t.Fatalf("the op answered %v; want one error_message", objs)
	}
	if got := objs[0].(astral.Error).Error(); !strings.Contains(got, "route not found") {
		t.Fatalf("error %q; want route not found", got)
	}
	if target := f.mod.joinTarget.Load(); target != nil {
		t.Fatalf("the join target is %v after the join; want none", target)
	}
}

// TestJoinWithARouteDoesNotScan: a swarm node that already resolves is joined
// at once, with no scan.
func TestJoinWithARouteDoesNotScan(t *testing.T) {
	f := newJoinFixture(t, astral.Err(errJoinDeclined))

	if _, err := routeJoin(t, f, f.node.id, "", "user.join?node=swarm"); err != nil {
		t.Fatalf("route: %v", err)
	}
	<-f.node.routed
	if n := f.nearby.scans.Load(); n != 0 {
		t.Fatalf("scanned %d times; want none", n)
	}
}
