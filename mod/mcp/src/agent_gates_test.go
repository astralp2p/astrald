package mcp

import (
	"errors"
	"net"
	"testing"

	authapi "github.com/astralp2p/astral-go/api/auth"
	mcpapi "github.com/astralp2p/astral-go/api/mcp"
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

	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	defer mod.unparkListener(agentID, ch)

	_, err = mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}

	// the listener survives: a parked listener is not permission, and a query
	// about to be refused must not consume one
	if _, err = mod.parkListener(agentID); !errors.Is(err, errAlreadyListening) {
		t.Fatal("the refused query popped the parked listener")
	}
}

// TestAnswerGateAsksAboutTheCalledAgent pins the actor. auth walks the contracts
// the actor is subject to, so naming the caller would search a stranger's
// delegations for a permission this agent's side holds.
func TestAnswerGateAsksAboutTheCalledAgent(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	agentID := registeredAgent(mod)

	q := inFlight(agentID, "chat")
	_, _ = mod.RouteQuery(mod.ctx, q, &bufWriteCloser{})

	if len(auth.asked) != 1 {
		t.Fatalf("the authority was asked %d times, want 1", len(auth.asked))
	}

	action, ok := auth.asked[0].(*mcpapi.AnswerAgentAction)
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

	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if len(auth.asked) != 0 {
		t.Fatalf("the authority was asked %d times about a target that is not an agent", len(auth.asked))
	}
}

// TestDisconnectAgentEndsLiveTraffic covers what the operation exists for: the
// conversations close and the queued queries go.
func TestDisconnectAgentEndsLiveTraffic(t *testing.T) {
	mod := testRouterModuleWithAuth(t, &fakeAuth{allow: true})
	agentID := registeredAgent(mod)

	local, remote := net.Pipe()
	defer remote.Close()

	s := mod.newSession(sessionInfo{
		agent:  agentID,
		conn:   testConn{Conn: local, local: agentID, remote: astral.GenerateIdentity()},
		caller: astral.GenerateIdentity(),
		format: sessionFormatRaw,
	})

	mod.enqueuePending(agentID, s)

	if mod.pendingCount(agentID) == 0 {
		t.Fatal("the query did not queue; the test proves nothing")
	}

	mod.dropPending(agentID)
	mod.closeAgentSessions(agentID)

	if mod.pendingCount(agentID) != 0 {
		t.Fatalf("pending: got %d, want 0", mod.pendingCount(agentID))
	}
	if _, live := mod.sessions.Get(s.id); live {
		t.Fatal("the session survived the disconnect")
	}
}

// TestDisconnectAgentLeavesTheParkedListener: a listener is the agent waiting to
// be called, not a caller it is talking to.
func TestDisconnectAgentLeavesTheParkedListener(t *testing.T) {
	mod := testRouterModuleWithAuth(t, &fakeAuth{allow: true})
	agentID := registeredAgent(mod)

	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	defer mod.unparkListener(agentID, ch)

	mod.dropPending(agentID)
	mod.closeAgentSessions(agentID)

	if _, err = mod.parkListener(agentID); !errors.Is(err, errAlreadyListening) {
		t.Fatal("the disconnect drained the agent's own listener")
	}
}

// TestCallActionNamesTheCallerAndTarget pins the outbound action's shape. The
// gate itself is exercised through the tool, which needs a node to route with;
// what belongs here is that the question asked is the right one.
func TestCallActionNamesTheCallerAndTarget(t *testing.T) {
	caller, target := astral.GenerateIdentity(), astral.GenerateIdentity()

	action := &mcpapi.CallAgentAction{
		Action: authapi.NewAction(caller),
		ToID:   target,
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("actor: got %v, want the calling agent %v", action.Actor(), caller)
	}
	if !action.ToID.IsEqual(target) {
		t.Fatalf("to: got %v, want %v", action.ToID, target)
	}
	if action.ObjectType() == (&mcpapi.AnswerAgentAction{}).ObjectType() {
		t.Fatal("both directions report one object type; auth cannot tell them apart")
	}
}
