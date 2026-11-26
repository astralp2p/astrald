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

// newContractFixture is an unclaimed node.
func newContractFixture() *bootstrapFixture {
	f := newBootstrapFixture()
	f.mod.log = log.New(nil)
	return f
}

// signedClaim is the fixture node's membership at tier from a fresh user,
// signed by both. forgeSubject signs as subject with a key other than the
// node's, which fails validation.
func signedClaim(t *testing.T, f *bootstrapFixture, tier user.Tier, forgeSubject bool) (*auth.SignedContract, *astral.Identity) {
	t.Helper()

	userKey := secp256k1.New()
	userID := secp256k1.Identity(secp256k1.PublicKey(userKey))
	contract, err := user.NewMembership(userID, f.nodeID, tier, nil, 24*time.Hour)
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

// A PrivilegedMembership the user key signed and this node countersigned
// claims the node; no policy is asked (D2).
func TestAcceptContractClaimsWithAPrivilegedMembership(t *testing.T) {
	f := newContractFixture()
	signed, userID := signedClaim(t, f, user.TierPrivilegedMember, false)

	if objs := routeContract(t, f, astral.GenerateIdentity(), signed); !answeredAck(objs) {
		t.Fatalf("the op answered %v, want ack", objs)
	}
	if ac := f.mod.ActiveContract(); ac == nil || !ac.Issuer.IsEqual(userID) {
		t.Fatal("the contract was not activated")
	}
}

// A contract that fails validation, or a membership below Privileged, is
// refused and nothing is claimed.
func TestAcceptContractRefusesAnInvalidClaim(t *testing.T) {
	for name, tc := range map[string]struct {
		tier   user.Tier
		forged bool
	}{
		"forged subject": {user.TierPrivilegedMember, true},
		"member tier":    {user.TierMember, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newContractFixture()
			signed, _ := signedClaim(t, f, tc.tier, tc.forged)

			objs := routeContract(t, f, astral.GenerateIdentity(), signed)
			if len(objs) != 1 {
				t.Fatalf("the op answered %v, want one error", objs)
			}
			if _, ok := objs[0].(astral.Error); !ok {
				t.Fatalf("the op answered %v, want an error", objs[0])
			}
			checkUnchanged(t, f)
		})
	}
}
