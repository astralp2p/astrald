package nodes

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// identityDir resolves an identity's own string and nothing else.
type identityDir struct{ dirmod.Module }

func (identityDir) ResolveIdentity(s string) (*astral.Identity, error) {
	return astral.ParseIdentity(s)
}

// resolveEnv is a nodes module, refused AdminNetworkAction, whose local swarm
// is this node and members, answering every resolve with one row.
type resolveEnv struct {
	mod  *Module
	auth *recordingAuth
}

func newResolveEnv(t *testing.T, members ...*astral.Identity) *resolveEnv {
	t.Helper()
	self := astral.GenerateIdentity()
	own := &ownResolver{}
	own.set(endpointsWithTTL("192.0.2.7")...)
	e := &resolveEnv{auth: &recordingAuth{verdict: false}}
	e.mod = &Module{
		Deps: Deps{Auth: e.auth, Dir: identityDir{},
			User: &swarmUser{swarm: append([]*astral.Identity{self}, members...)}},
		node: &identityNode{id: self},
		log:  log.New(self),
	}
	e.mod.resolvers.Add(own)
	return e
}

// answer routes nodes.resolve_endpoints for target as caller and returns the
// first object the op sent.
func (e *resolveEnv) answer(t *testing.T, caller, target *astral.Identity) astral.Object {
	t.Helper()
	w := newRecordingWriter()
	if err := route(t, e.mod.OpResolveEndpoints, caller, "nodes.resolve_endpoints?identity="+target.String(), w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	obj, err := channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
	if err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return obj
}

func requireRefused(t *testing.T, obj astral.Object) {
	t.Helper()
	msg, ok := obj.(*astral.ErrorMessage)
	if !ok || msg.Error() != auth.ErrNotPermitted.Error() {
		t.Fatalf("answered %v; want %v", obj, auth.ErrNotPermitted)
	}
}

// TestResolveEndpointsAnswersAMemberAboutAMember: a swarm member without
// AdminNetworkAction reads the endpoints of another member and of the
// answering node.
//
// why: endpoints travel only through nodes.resolve_endpoints, so a Member
// refused here never links to another Member (swarm-four-siblings).
func TestResolveEndpointsAnswersAMemberAboutAMember(t *testing.T) {
	caller, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newResolveEnv(t, caller, other)

	for name, target := range map[string]*astral.Identity{"member": other, "answering node": e.mod.node.Identity()} {
		t.Run(name, func(t *testing.T) {
			if _, ok := e.answer(t, caller, target).(*nodes.EndpointWithTTL); !ok {
				t.Fatal("refused a member's read of a member's endpoints")
			}
		})
	}
}

// TestResolveEndpointsRefusesOutsidersAndNonMemberTargets: without
// AdminNetworkAction, a caller outside the swarm is refused, and a member is
// refused the endpoints of a node outside the swarm.
func TestResolveEndpointsRefusesOutsidersAndNonMemberTargets(t *testing.T) {
	member, outsider := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newResolveEnv(t, member)

	t.Run("outsider about a member", func(t *testing.T) {
		requireRefused(t, e.answer(t, outsider, member))
	})
	t.Run("outsider about the answering node", func(t *testing.T) {
		requireRefused(t, e.answer(t, outsider, e.mod.node.Identity()))
	})
	t.Run("member about an outsider", func(t *testing.T) {
		requireRefused(t, e.answer(t, member, outsider))
	})
}

// TestLearnMemberEndpointsPullsFromLinkedMembersUntilNewRows: a new member the
// node holds no link with is pulled from the linked members, one at a time,
// until one stores a new row; the node then emits an EndpointsUpdatedEvent.
func TestLearnMemberEndpointsPullsFromLinkedMembersUntilNewRows(t *testing.T) {
	f := newExchangeFixture(t)
	first := f.member(true)
	second := f.member(true)
	f.member(false)
	subject := f.member(false)

	var calls []*astral.Identity
	f.mod.resolveRemote = func(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		if !identity.IsEqual(subject) {
			t.Errorf("pulled endpoints of %v; want of %v", identity, subject)
		}
		calls = append(calls, resolver)
		if len(calls) == 1 {
			return nil, errors.New("not permitted")
		}
		return endpointsWithTTL("198.51.100.9"), nil
	}

	f.mod.LearnMemberEndpoints(subject)

	if len(calls) != 2 {
		t.Fatalf("made %d pulls; want 2 (a refusal, then a pull with new rows)", len(calls))
	}
	if !(calls[0].IsEqual(first) && calls[1].IsEqual(second)) && !(calls[0].IsEqual(second) && calls[1].IsEqual(first)) {
		t.Fatalf("pulled from %v; want the two linked members", calls)
	}
	emitted := f.events.recorded()
	if len(emitted) != 1 {
		t.Fatalf("emitted %v events; want one EndpointsUpdatedEvent", len(emitted))
	}
	if e, ok := emitted[0].(*nodes.EndpointsUpdatedEvent); !ok || !e.Identity.IsEqual(subject) {
		t.Fatalf("emitted %v; want an EndpointsUpdatedEvent for %v", emitted[0], subject)
	}
}

// TestLearnMemberEndpointsOfALinkedMemberIsFirstHand: a new member the node is
// linked to is pulled from itself, and the notice goes to the other linked
// members, as on a first link.
func TestLearnMemberEndpointsOfALinkedMemberIsFirstHand(t *testing.T) {
	f := newExchangeFixture(t)
	subject := f.member(true)
	sibling := f.member(true)

	f.mod.LearnMemberEndpoints(subject)

	f.requirePull(t, subject, subject)
	eventually(t, "forward", func() bool { return len(f.objects.pushes.list()) > 0 })
	time.Sleep(absenceWindow)

	forwards := f.objects.pushes.list()
	if len(forwards) != 1 || !forwards[0].target.IsEqual(sibling) {
		t.Fatalf("forwards %v; want the notice to %v alone", forwards, sibling)
	}
	if m, ok := forwards[0].object.(*nodes.EndpointsChangedMessage); !ok || !m.Subject.IsEqual(subject) {
		t.Fatalf("forwarded %v; want the notice of %v", forwards[0].object, subject)
	}
}
