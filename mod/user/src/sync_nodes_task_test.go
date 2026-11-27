package user

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// syncSteps is the ordered record of every step a sync took with the remote node.
type syncSteps struct {
	mu    sync.Mutex
	steps []string
}

func (s *syncSteps) add(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *syncSteps) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// stepPusher is an objects module that records each push as a step named by the
// object's kind.
type stepPusher struct {
	objectsmod.Module
	steps *syncSteps
	kind  func(astral.Object) string
}

func (p *stepPusher) Push(_ *astral.Context, _ *astral.Identity, obj astral.Object) error {
	p.steps.add("push " + p.kind(obj))
	return nil
}

// stepNode is the local node; it records each query it routes as a step named by
// the op and answers user.info with an empty info. A non-nil refusal answers every query instead.
type stepNode struct {
	astral.Node
	id      *astral.Identity
	steps   *syncSteps
	refusal astral.Error
	// callers holds the caller of each query, by op
	callers sync.Map
}

func (n *stepNode) Identity() *astral.Identity { return n.id }

func (n *stepNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	op, _ := query.Parse(q.QueryString.String())
	n.steps.add("query " + op)
	n.callers.Store(op, q.Caller)

	var answer astral.Object = &user.Info{}
	switch {
	case n.refusal != nil:
		answer = n.refusal
	}

	return query.Accept(q, w, func(conn astral.Conn) {
		ch := channel.New(conn)
		defer ch.Close()
		ch.Send(answer)
	})
}

// syncFixture is a node of user U holding its active contract, one sibling's
// contract, the relay contract of one app it hosts, and one ban. Each push is a
// step named by what the pushed object is.
type syncFixture struct {
	mod   *Module
	steps *syncSteps
}

func newSyncFixture(t *testing.T, ctx *astral.Context) *syncFixture {
	t.Helper()

	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	active := nodeContract(userID, nodeID)
	app := relayForContract(astral.GenerateIdentity(), nodeID)
	names := map[astral.Object]string{active: "active contract", app: "app contract"}

	index := &indexAuth{}
	index.seed(app)

	f := &syncFixture{steps: &syncSteps{}}
	kind := func(obj astral.Object) string {
		if name, ok := names[obj]; ok {
			return name
		}
		if _, ok := obj.(*user.SignedExpulsion); ok {
			return "expulsion"
		}
		return obj.ObjectType()
	}
	f.mod = &Module{
		Deps: Deps{Auth: index, Objects: &stepPusher{steps: f.steps, kind: kind}},
		ctx:  ctx,
		node: &stepNode{id: nodeID, steps: f.steps},
		log:  log.New(nodeID),
		db:   testDB(t),
	}
	if err := f.mod.config.ActiveContract.Set(nil, active); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	openStore(t, f.mod, time.Now, realRules)
	for _, id := range []*astral.Identity{nodeID, astral.GenerateIdentity()} {
		storeMembership(t, f.mod, userID, id, user.TierMember, time.Now().Add(time.Hour))
	}
	banNode(t, f.mod, userID, astral.GenerateIdentity())
	return f
}

// TestSyncNodesTaskPushesTheContractFirst: a sync pushes this node's active
// contract, then the bans, then pulls the swarm digest, and only then queries the
// remote node for the alias, asks it for its relay digest and pushes the
// app contracts. The step node answers the digest queries with a
// user.info, which is no digest, so the pull ends and the app contract is pushed.
func TestSyncNodesTaskPushesTheContractFirst(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()
	f := newSyncFixture(t, ctx)

	if err := f.mod.NewSyncNodesTask(astral.GenerateIdentity()).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{
		"push active contract",
		"push expulsion",
		"query user." + opSwarmDigest,
		"query " + user.OpInfo,
		"query " + relayDigestOp,
		"push app contract",
	}
	if got := f.steps.list(); !slices.Equal(got, want) {
		t.Fatalf("sync steps %q; want %q", got, want)
	}
}

// TestSyncAliasReportsARefusal: an error_message the remote node answers to
// user.info reads as its refusal, not as a protocol error.
func TestSyncAliasReportsARefusal(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()
	f := newSyncFixture(t, ctx)
	f.mod.node.(*stepNode).refusal = auth.ErrNotPermitted

	err := f.mod.syncAlias(ctx, astral.GenerateIdentity())
	if err == nil || err.Error() != "refused: "+auth.ErrNotPermitted.Error() || !errors.Is(err, auth.ErrNotPermitted) {
		t.Fatalf("syncAlias answered %v; want the remote node's refusal", err)
	}
}

// TestSyncAliasAsksAsTheNode: syncAlias asks the sibling's user.info as this
// node, not as the user: a Member may not relay as the user (D5, D6), so a
// query as the user from a Member never reaches the op.
func TestSyncAliasAsksAsTheNode(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()
	f := newSyncFixture(t, ctx)
	node := f.mod.node.(*stepNode)

	if err := f.mod.syncAlias(ctx, astral.GenerateIdentity()); err != nil {
		t.Fatalf("syncAlias: %v", err)
	}
	got, ok := node.callers.Load(user.OpInfo)
	if !ok {
		t.Fatal("syncAlias asked no user.info")
	}
	if caller := got.(*astral.Identity); !caller.IsEqual(node.id) {
		t.Fatalf("user.info asked as %v; want this node %v", caller, node.id)
	}
}
