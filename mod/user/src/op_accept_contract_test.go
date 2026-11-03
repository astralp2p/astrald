package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// newContractFixture is an unclaimed node whose queries the delegate answers
// with answer; a nil answer leaves every question open.
func newContractFixture(answer astral.Object) (*bootstrapFixture, *swarmDelegate) {
	f := newBootstrapFixture()
	node := newSwarmDelegate(answer)
	node.id = f.nodeID
	f.mod.node = node
	f.mod.log = log.New(nil)
	return f, node
}

// signedClaim is a node contract from a fresh user to the fixture's node,
// signed by both. forgeSubject signs as subject with a key other than the
// node's, which fails validation.
func signedClaim(t *testing.T, f *bootstrapFixture, forgeSubject bool) (*auth.SignedContract, *astral.Identity) {
	t.Helper()

	userKey := secp256k1.New()
	userID := secp256k1.Identity(secp256k1.PublicKey(userKey))
	contract, err := user.NewNodeContract(userID, f.nodeID, true, 24*time.Hour)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	sc := &auth.SignedContract{Contract: contract}
	if sc.IssuerSig, err = secp256k1sig.SignASN1(userKey, sc.SignableHash()); err != nil {
		t.Fatalf("issuer sig: %v", err)
	}
	nodeKey := f.auth.nodeKey
	if forgeSubject {
		nodeKey = secp256k1.New()
	}
	if sc.SubjectSig, err = secp256k1sig.SignASN1(nodeKey, sc.SignableHash()); err != nil {
		t.Fatalf("subject sig: %v", err)
	}
	return sc, userID
}

// routeContract sends signed to user.accept_contract from caller and answers
// what the op wrote. The caller stays connected until the op returns, as
// astral-go's client does while it waits for the answer.
func routeContract(t *testing.T, f *bootstrapFixture, caller *astral.Identity, signed *auth.SignedContract) []astral.Object {
	t.Helper()

	op, err := routing.NewOp(f.mod.OpAcceptContract)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	w := &answerWriter{}
	requester, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, f.nodeID, user.OpAcceptContract, nil)), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	defer requester.Close()
	if err = channel.NewSender(requester).Send(signed); err != nil {
		t.Fatalf("send contract: %v", err)
	}

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}
	return w.objects(t)
}

func answeredAck(objs []astral.Object) bool {
	if len(objs) != 1 {
		return false
	}
	_, ok := objs[0].(*astral.Ack)
	return ok
}

func answeredDeclined(objs []astral.Object) bool {
	if len(objs) != 1 {
		return false
	}
	e, ok := objs[0].(astral.Error)
	return ok && e.Error() == user.ErrContractDeclined.Error()
}

// With no delegate set, a valid contract claims the node: a fresh node stays
// claimable.
func TestAcceptContractDefaultPolicyClaims(t *testing.T) {
	f, _ := newContractFixture(nil)
	signed, userID := signedClaim(t, f, false)

	if objs := routeContract(t, f, astral.GenerateIdentity(), signed); !answeredAck(objs) {
		t.Fatalf("the op answered %v, want ack", objs)
	}
	if ac := f.mod.ActiveContract(); ac == nil || !ac.Issuer.IsEqual(userID) {
		t.Fatal("the contract was not activated")
	}
}

// A delegate that declines leaves the node unclaimed and nothing stored.
func TestAcceptContractDelegateDeclines(t *testing.T) {
	f, node := newContractFixture(&user.ContractAcceptDecision{Allow: false})
	if err := f.mod.policy.ContractAccept.Set(astral.NewContext(nil), delegate()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	signed, _ := signedClaim(t, f, false)

	if objs := routeContract(t, f, astral.GenerateIdentity(), signed); !answeredDeclined(objs) {
		t.Fatalf("the op answered %v, want %q", objs, user.ErrContractDeclined)
	}
	if op := <-node.queried; op != "control.decide_swarm" {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	checkUnchanged(t, f)
}

// A delegate that approves sees the caller and the signed contract, and the
// node is claimed.
func TestAcceptContractDelegateApproves(t *testing.T) {
	f, node := newContractFixture(&user.ContractAcceptDecision{Allow: true})
	if err := f.mod.policy.ContractAccept.Set(astral.NewContext(nil), delegate()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	signed, userID := signedClaim(t, f, false)
	caller := astral.GenerateIdentity()

	if objs := routeContract(t, f, caller, signed); !answeredAck(objs) {
		t.Fatalf("the op answered %v, want ack", objs)
	}
	req, ok := (<-node.got).(*user.ContractAcceptRequest)
	if !ok || !req.Caller.IsEqual(caller) || !req.Contract.Issuer.IsEqual(userID) || req.Contract.SubjectSig == nil {
		t.Fatal("the delegate did not receive the caller and the signed contract")
	}
	if ac := f.mod.ActiveContract(); ac == nil || !ac.Issuer.IsEqual(userID) {
		t.Fatal("the approved contract was not activated")
	}
}

// A contract that fails validation is refused before the delegate is asked.
func TestAcceptContractValidatesBeforeThePolicy(t *testing.T) {
	f, node := newContractFixture(&user.ContractAcceptDecision{Allow: true})
	if err := f.mod.policy.ContractAccept.Set(astral.NewContext(nil), delegate()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	signed, _ := signedClaim(t, f, true)

	objs := routeContract(t, f, astral.GenerateIdentity(), signed)
	if len(objs) != 1 || answeredDeclined(objs) {
		t.Fatalf("the op answered %v, want the validation error", objs)
	}
	if _, ok := objs[0].(astral.Error); !ok {
		t.Fatalf("the op answered %v, want an error", objs[0])
	}
	select {
	case op := <-node.queried:
		t.Fatalf("an invalid contract reached the delegate (%q)", op)
	default:
	}
	checkUnchanged(t, f)
}

// An answer of the wrong type is an error, never permission.
func TestContractAcceptDelegateRefusesAnUnreadableAnswer(t *testing.T) {
	mod := &Module{node: newSwarmDelegate(&user.SwarmInviteDecision{Allow: true}), log: log.New(nil)}

	_, err := mod.ContractAcceptPolicyDelegated(delegate())(astral.NewContext(nil), &user.ContractAcceptRequest{
		Caller: astral.GenerateIdentity(), Contract: &auth.SignedContract{Contract: &auth.Contract{}},
	})
	if err == nil {
		t.Fatal("an invite decision was read as a contract accept decision")
	}
}
