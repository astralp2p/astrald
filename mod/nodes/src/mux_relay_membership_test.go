package nodes

import (
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	authapi "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/nodes/frames"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// refusingAuth counts Authorize calls and refuses every action, so a relay
// that reaches the contract chain walk is answered CodeRejected after it.
type refusingAuth struct {
	authmod.Module
	calls atomic.Int32
}

func (a *refusingAuth) Authorize(*astral.Context, authapi.ActionObject) bool {
	a.calls.Add(1)
	return false
}

// claimUser is a user module of a node claimed by user whose local swarm is
// members; a nil user is an unclaimed node.
type claimUser struct {
	usermod.Module
	user    *astral.Identity
	members []*astral.Identity
}

func (u *claimUser) Identity() *astral.Identity { return u.user }

func (u *claimUser) IsSwarmMember(id *astral.Identity) bool {
	return u.user != nil && slices.ContainsFunc(u.members, id.IsEqual)
}

// relayVerdict sends one RelayQuery frame for caller from the remote end of a
// mux on a node with user, and returns the response code and the number of
// Authorize calls the node made.
func relayVerdict(t *testing.T, user usermod.Module, localID, remoteID, caller *astral.Identity) (astral.Uint8, int32) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })

	auth := &refusingAuth{}
	m := &Mux{
		mod:            &Module{Deps: Deps{Auth: auth, User: user}, node: &identityNode{id: localID}, log: log.New(localID)},
		ch:             channel.New(a, channel.WithLockedWrites()),
		localIdentity:  localID,
		remoteIdentity: remoteID,
		routerSet:      make(chan struct{}),
	}
	m.SetRouter(&answeringRouter{reply: "answer", routed: make(chan *astral.InFlightQuery, 1)})
	go func() {
		for {
			obj, err := m.ch.Receive()
			if err != nil || m.Handle(obj) != nil {
				return
			}
		}
	}()

	peer := channel.New(b, channel.WithLockedWrites())
	nonce := astral.NewNonce()
	err := peer.Send(&frames.RelayQuery{
		CallerID: caller,
		TargetID: localID,
		Query:    frames.Query{Nonce: nonce, Query: "ping"},
	})
	if err != nil {
		t.Fatalf("send relay query: %v", err)
	}

	got := make(chan *frames.Response, 1)
	go func() {
		obj, err := peer.Receive()
		if err != nil {
			return
		}
		if r, ok := obj.(*frames.Response); ok && r.Nonce == nonce {
			got <- r
		}
	}()
	select {
	case r := <-got:
		return astral.Uint8(r.ErrCode), auth.calls.Load()
	case <-time.After(effectTimeout):
		t.Fatal("no response to the relay query")
	}
	return 0, 0
}

// TestRelayFromAFormerMemberIsRefusedBeforeTheChainWalk: on a claimed node, a
// relaying node that is not a current swarm member gets CodeRejected and the
// contract chain is never consulted (A2G8, astral-docs topics/app-routing.md,
// Authorizing the relay).
func TestRelayFromAFormerMemberIsRefusedBeforeTheChainWalk(t *testing.T) {
	self, former, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	user := &claimUser{user: astral.GenerateIdentity(), members: []*astral.Identity{self}}

	code, calls := relayVerdict(t, user, self, former, app)
	if code != astral.Uint8(frames.CodeRejected) {
		t.Fatalf("response code %v; want CodeRejected %v", code, frames.CodeRejected)
	}
	if calls != 0 {
		t.Fatalf("Authorize called %d times for a non-member relayer; want 0", calls)
	}
}

// TestRelayFromAMemberOrToAnUnclaimedNodeReachesTheChainWalk: a member's relay
// on a claimed node, and any relay on an unclaimed node, reach Authorize.
func TestRelayFromAMemberOrToAnUnclaimedNodeReachesTheChainWalk(t *testing.T) {
	self, relayer, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	for name, user := range map[string]usermod.Module{
		"member on a claimed node": &claimUser{user: astral.GenerateIdentity(), members: []*astral.Identity{self, relayer}},
		"unclaimed node":           &claimUser{},
		"no user module":           nil,
	} {
		code, calls := relayVerdict(t, user, self, relayer, app)
		if calls != 1 {
			t.Errorf("%s: Authorize called %d times; want 1", name, calls)
		}
		if code != astral.Uint8(frames.CodeRejected) {
			t.Errorf("%s: response code %v; want the chain walk's CodeRejected", name, code)
		}
	}
}

// TestRelayAsItselfSkipsTheMembershipCheck: a frame whose caller is the remote
// node itself is no relay, so a non-member's own query passes both checks and
// is routed.
func TestRelayAsItselfSkipsTheMembershipCheck(t *testing.T) {
	self, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()
	user := &claimUser{user: astral.GenerateIdentity(), members: []*astral.Identity{self}}

	code, calls := relayVerdict(t, user, self, stranger, stranger)
	if code != 0 {
		t.Fatalf("response code %v; want 0, the query routed", code)
	}
	if calls != 0 {
		t.Fatalf("Authorize called %d times for a query as the remote itself; want 0", calls)
	}
}
