package user

import (
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// askRelayContracts sends user.relay_contracts for issuer from caller to f's
// node and returns the answer.
func askRelayContracts(t *testing.T, f *receiverFixture, caller, issuer *astral.Identity) []astral.Object {
	t.Helper()

	objs, err := routeOp(t, f.mod.OpRelayContracts,
		query.New(caller, f.nodeID, user.OpRelayContracts, query.Args{"issuer": issuer}), "", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	return objs
}

// TestRelayContractsAnswersOnlyDeliverableContractsOfTheIssuer: a member reads
// the relay contracts of the issuer it names that another member admits, then
// eos; every other contract the index holds stays out.
func TestRelayContractsAnswersOnlyDeliverableContractsOfTheIssuer(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	caller, host := f.addSibling(), f.addSibling()
	app := astral.GenerateIdentity()

	want := relayForContract(app, host)
	f.auth.seed(want)

	expired := relayForContract(app, host)
	expired.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
	forged := relayForContract(app, host)
	forged.SubjectSig = forgedSig()
	for _, sc := range []*auth.SignedContract{
		relayForContract(astral.GenerateIdentity(), host),                                       // another issuer
		relayForContract(app, astral.GenerateIdentity()),                                        // subject outside the swarm
		relayForContract(app, f.expelSibling(t)),                                                // expelled subject
		relayForContract(app, caller),                                                           // the caller's own
		contractWith(app, host, nodes.RelayForAction{}.ObjectType(), "mod.objects.read_action"), // another permit beside
		contractWith(app, host, "mod.objects.read_action"),                                      // no relay-for permit
		expired,
		forged,
	} {
		f.auth.seed(sc)
	}

	objs := askRelayContracts(t, f, caller, app)
	if len(objs) != 2 {
		t.Fatalf("answered %d objects (%v); want one contract and eos", len(objs), objs)
	}
	got, ok := objs[0].(*auth.SignedContract)
	if !ok {
		t.Fatalf("answered %v; want the deliverable contract", objs[0])
	}
	gotID, err := astral.ResolveObjectID(got)
	if err != nil {
		t.Fatal(err)
	}
	wantID, err := astral.ResolveObjectID(want)
	if err != nil {
		t.Fatal(err)
	}
	if !gotID.IsEqual(wantID) {
		t.Fatalf("answered contract %v; want the deliverable contract %v", gotID, wantID)
	}
	if _, ok := objs[1].(*astral.EOS); !ok {
		t.Fatalf("ended with %v; want eos", objs[1])
	}
}

// TestRelayContractsNoneIsEOS: a member asking for an issuer with no relay
// contract reads eos alone.
func TestRelayContractsNoneIsEOS(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())

	objs := askRelayContracts(t, f, f.addSibling(), astral.GenerateIdentity())
	if len(objs) != 1 {
		t.Fatalf("answered %v; want eos alone", objs)
	}
	if _, ok := objs[0].(*astral.EOS); !ok {
		t.Fatalf("answered %v; want eos", objs[0])
	}
}

// TestRelayContractsRefusesNonMembers: a caller outside the local swarm, an
// expelled member, the user, and the node itself are answered not permitted,
// also when the index holds a deliverable contract.
func TestRelayContractsRefusesNonMembers(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	f.auth.seed(nodeContract(userID, f.nodeID))
	app := astral.GenerateIdentity()
	f.auth.seed(relayForContract(app, f.addSibling()))

	for name, caller := range map[string]*astral.Identity{
		"outside the swarm": astral.GenerateIdentity(),
		"expelled":          f.expelSibling(t),
		"the user":          userID,
		"this node":         f.nodeID,
	} {
		t.Run(name, func(t *testing.T) {
			objs := askRelayContracts(t, f, caller, app)
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

// TestRelayContractsIsMounted: the module serves the op under the name the
// astral-go client queries, inside the module's scope.
func TestRelayContractsIsMounted(t *testing.T) {
	mod := &Module{}
	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		t.Fatalf("mount: %v", err)
	}

	name, ok := strings.CutPrefix(user.OpRelayContracts, usermod.ModuleName+".")
	if !ok || !mod.router.HasRoute(name) {
		t.Fatalf("no route %q in scope %q for %q", name, usermod.ModuleName, user.OpRelayContracts)
	}
}
