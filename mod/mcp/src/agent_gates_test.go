package mcp

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// registeredAgent makes an identity one this module answers for. Registration is
// the whole of what the node holds about reachability; who the agent answers is
// the gate under test.
func registeredAgent(mod *Module) *astral.Identity {
	id := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(id.String())
	return id
}

// TestAnswerGateRefusesWhatTheAuthorityRefuses: the agent is one this module
// answers for, and its own side turns the caller away.
func TestAnswerGateRefusesWhatTheAuthorityRefuses(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	agentID := registeredAgent(mod)

	_, err := mod.RouteQuery(mod.ctx, inFlight(agentID, mcp.MethodMessage), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
}

// TestAnswerGateAsksAboutTheCalledAgent pins the actor. auth walks the contracts
// the actor is subject to, so naming the caller would search a stranger's
// delegations for a permission this agent's side holds.
func TestAnswerGateAsksAboutTheCalledAgent(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	agentID := registeredAgent(mod)

	q := inFlight(agentID, mcp.MethodMessage)
	_, _ = mod.RouteQuery(mod.ctx, q, &bufWriteCloser{})

	if len(auth.asked) != 1 {
		t.Fatalf("the authority was asked %d times, want 1", len(auth.asked))
	}

	action, ok := auth.asked[0].(*mcp.AnswerAgentAction)
	if !ok {
		t.Fatalf("action: got %T, want *mcp.AnswerAgentAction", auth.asked[0])
	}
	if !action.Actor().IsEqual(agentID) {
		t.Fatalf("actor: got %v, want the called agent %v", action.Actor(), agentID)
	}
	if !action.FromID.IsEqual(q.Caller) {
		t.Fatalf("from: got %v, want the caller %v", action.FromID, q.Caller)
	}
}

// TestAnswerGateIsNotReachedForAnUnregisteredTarget keeps the question off a
// target this module does not answer for: it belongs to the other routers, and
// an authority has nothing to say about it.
func TestAnswerGateIsNotReachedForAnUnregisteredTarget(t *testing.T) {
	auth := &fakeAuth{allow: true}
	mod := testRouterModuleWithAuth(t, auth)

	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), mcp.MethodMessage), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if len(auth.asked) != 0 {
		t.Fatalf("the authority was asked %d times about a target that is not an agent", len(auth.asked))
	}
}

// TestCallActionNamesTheCallerAndTarget pins the outbound action's shape. The
// gate itself is exercised through the tool, which needs a node to route with;
// what belongs here is that the question asked is the right one.
func TestCallActionNamesTheCallerAndTarget(t *testing.T) {
	caller, target := astral.GenerateIdentity(), astral.GenerateIdentity()

	action := &mcp.CallAgentAction{
		Action: auth.NewAction(caller),
		ToID:   target,
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("actor: got %v, want the calling agent %v", action.Actor(), caller)
	}
	if !action.ToID.IsEqual(target) {
		t.Fatalf("to: got %v, want %v", action.ToID, target)
	}
	if action.ObjectType() == (&mcp.AnswerAgentAction{}).ObjectType() {
		t.Fatal("both directions report one object type; auth cannot tell them apart")
	}
}
