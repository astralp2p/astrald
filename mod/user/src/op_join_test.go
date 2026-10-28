package user

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// swarmNode is the unclaimed node user.join runs on, linked to one swarm node.
// Every query it routes reaches the swarm node, which answers it with answer;
// the node records each routed query and the zone it was routed in.
type swarmNode struct {
	astral.Node
	id     *astral.Identity
	answer astral.Object
	routed chan routedQuery
}

type routedQuery struct {
	query *astral.Query
	zone  astral.Zone
}

func (n *swarmNode) Identity() *astral.Identity { return n.id }

func (n *swarmNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.routed <- routedQuery{query: q.Query, zone: ctx.Zone()}
	op, err := routing.NewOp(func(_ *astral.Context, q *routing.IncomingQuery) error {
		return q.Accept().Send(n.answer)
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

type joinFixture struct {
	mod     *Module
	node    *swarmNode
	swarmID *astral.Identity
}

// newJoinFixture is an unclaimed node that names its swarm node "swarm", the
// zero identity "anyone" and itself "self".
func newJoinFixture(answer astral.Object) *joinFixture {
	f := &joinFixture{swarmID: astral.GenerateIdentity()}
	f.node = &swarmNode{id: astral.GenerateIdentity(), answer: answer, routed: make(chan routedQuery, 1)}
	f.mod = &Module{
		Deps: Deps{Dir: &namedDir{names: map[string]*astral.Identity{
			"swarm":  f.swarmID,
			"anyone": astral.Anyone,
			"self":   f.node.id,
		}}},
		node: f.node,
	}
	return f
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

// TestJoinRequestsMembershipAsTheNode: user.join sends user.request_membership
// to the named node, as this node and with the Network zone, and answers the
// contract the swarm node returns.
func TestJoinRequestsMembershipAsTheNode(t *testing.T) {
	signed := &auth.SignedContract{Contract: &auth.Contract{Issuer: astral.GenerateIdentity()}}
	f := newJoinFixture(signed)

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
	if op, _ := query.Parse(routed.query.QueryString.String()); op != user.OpRequestMembership {
		t.Fatalf("request %q; want %v", routed.query.QueryString, user.OpRequestMembership)
	}
	if !routed.zone.Is(astral.ZoneNetwork) {
		t.Fatalf("request routed in zone %v; want the Network zone", routed.zone)
	}

	if len(objs) != 1 {
		t.Fatalf("the op answered %d objects (%v); want one contract", len(objs), objs)
	}
	got, ok := objs[0].(*auth.SignedContract)
	if !ok || !got.Issuer.IsEqual(signed.Issuer) {
		t.Fatalf("the op answered %v; want the swarm node's contract", objs[0])
	}
}

// TestJoinAnswersTheSwarmNodesRefusal: a request the swarm node declines is
// answered with the swarm node's error.
func TestJoinAnswersTheSwarmNodesRefusal(t *testing.T) {
	f := newJoinFixture(user.ErrRequestDeclined)

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

// TestJoinRejects: every refused query is rejected with a code, or answered with
// a single error_message, and no request leaves the node.
func TestJoinRejects(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		caller func(f *joinFixture) *astral.Identity
		node   string
		code   uint8
		answer astral.Error
	}{
		{"network origin", astral.OriginNetwork, func(f *joinFixture) *astral.Identity { return f.node.id }, "swarm", astral.CodeRejected, nil},
		{"mcp origin", astral.OriginMCP, func(f *joinFixture) *astral.Identity { return f.node.id }, "swarm", astral.CodeRejected, nil},
		{"caller other than the node", "", func(*joinFixture) *astral.Identity { return astral.GenerateIdentity() }, "swarm", 4, nil},
		{"unknown node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "nobody", 0, dir.ErrUnknownIdentity},
		{"anonymous node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "anyone", 0, astral.ErrZeroIdentity},
		{"this node", "", func(f *joinFixture) *astral.Identity { return f.node.id }, "self", 0, user.ErrJoinSelf},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newJoinFixture(&auth.SignedContract{})

			objs, err := routeJoin(t, f, c.caller(f), c.origin, "user.join?node="+c.node)

			if c.answer != nil {
				if err != nil {
					t.Fatalf("route: %v", err)
				}
				if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != c.answer.Error() {
					t.Fatalf("the op answered %v; want %q", objs, c.answer)
				}
			} else {
				var rejected *astral.ErrRejected
				if !errors.As(err, &rejected) || rejected.Code != c.code {
					t.Fatalf("got %v; want a rejection with code %d", err, c.code)
				}
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
	f := newJoinFixture(&auth.SignedContract{})
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
