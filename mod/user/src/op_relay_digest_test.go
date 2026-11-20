package user

import (
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// askRelayDigestOp sends user.relay_digest from caller to f's node and returns
// the answer.
func askRelayDigestOp(t *testing.T, f *receiverFixture, caller *astral.Identity) []astral.Object {
	t.Helper()

	objs, err := routeOp(t, f.mod.OpRelayDigest, query.New(caller, f.nodeID, relayDigestOp, nil), "", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	return objs
}

func objectIDOf(t *testing.T, sc *auth.SignedContract) astral.ObjectID {
	t.Helper()

	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatal(err)
	}
	return *id
}

// TestRelayDigestAnswersTheIDsOfTheHeldRelayContracts: a member reads the object
// ID of every relay-for contract the index holds, whoever issued it, hosts it or
// would be pushed it, then eos; a contract with no relay-for permit and one that
// carries another permit beside relay_for stay out.
func TestRelayDigestAnswersTheIDsOfTheHeldRelayContracts(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	caller, host := f.addSibling(), f.addSibling()

	held := []*auth.SignedContract{
		relayForContract(astral.GenerateIdentity(), host),
		relayForContract(astral.GenerateIdentity(), caller),                    // the caller's own
		relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity()), // subject outside the swarm
	}
	for _, sc := range held {
		f.auth.seed(sc)
	}
	f.auth.seed(contractWith(astral.GenerateIdentity(), host, "mod.objects.read_action")) // no relay-for permit
	f.auth.seed(contractWith(astral.GenerateIdentity(), host,
		nodes.RelayForAction{}.ObjectType(), "mod.objects.read_action")) // another permit beside relay_for

	objs := askRelayDigestOp(t, f, caller)
	if len(objs) != len(held)+1 {
		t.Fatalf("answered %d objects (%v); want %d ids and eos", len(objs), objs, len(held))
	}
	if _, ok := objs[len(objs)-1].(*astral.EOS); !ok {
		t.Fatalf("ended with %v; want eos", objs[len(objs)-1])
	}

	got := map[astral.ObjectID]bool{}
	for _, obj := range objs[:len(objs)-1] {
		id, ok := obj.(*astral.ObjectID)
		if !ok {
			t.Fatalf("answered %v; want an object id", obj)
		}
		got[*id] = true
	}
	for _, sc := range held {
		if !got[objectIDOf(t, sc)] {
			t.Errorf("the digest lacks the id of %v", sc.Subject)
		}
	}
	if len(got) != len(held) {
		t.Errorf("the digest lists %d ids; want %d", len(got), len(held))
	}
}

// TestRelayDigestNoneIsEOS: a node that holds no relay contract answers eos alone.
func TestRelayDigestNoneIsEOS(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())

	objs := askRelayDigestOp(t, f, f.addSibling())
	if len(objs) != 1 {
		t.Fatalf("answered %v; want eos alone", objs)
	}
	if _, ok := objs[0].(*astral.EOS); !ok {
		t.Fatalf("answered %v; want eos", objs[0])
	}
}

// TestRelayDigestRefusesNonMembers: a caller outside the local swarm, an
// expelled member, the user, and the node itself are answered not permitted,
// also when the index holds a relay contract.
func TestRelayDigestRefusesNonMembers(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	f.auth.seed(nodeContract(userID, f.nodeID))
	f.auth.seed(relayForContract(astral.GenerateIdentity(), f.addSibling()))

	for name, caller := range map[string]*astral.Identity{
		"outside the swarm": astral.GenerateIdentity(),
		"expelled":          f.expelSibling(),
		"the user":          userID,
		"this node":         f.nodeID,
	} {
		t.Run(name, func(t *testing.T) {
			objs := askRelayDigestOp(t, f, caller)
			if len(objs) != 1 {
				t.Fatalf("answered %v; want the refusal alone", objs)
			}
			got, ok := objs[0].(astral.Error)
			if !ok || got.Error() != auth.ErrNotPermitted.Error() {
				t.Fatalf("answered %v; want %q", objs[0], auth.ErrNotPermitted)
			}
		})
	}
}

// TestRelayDigestIsMounted: the module serves the op under relayDigestOp, inside
// the module's scope.
func TestRelayDigestIsMounted(t *testing.T) {
	mod := &Module{}
	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		t.Fatalf("mount: %v", err)
	}

	name, ok := strings.CutPrefix(relayDigestOp, usermod.ModuleName+".")
	if !ok || !mod.router.HasRoute(name) {
		t.Fatalf("no route %q in scope %q for %q", name, usermod.ModuleName, relayDigestOp)
	}
}
