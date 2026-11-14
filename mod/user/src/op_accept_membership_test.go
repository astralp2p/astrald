package user

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/lib/policyconf"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// racingAuth is the auth module user.accept_membership reaches. VerifyIssuer
// holds every caller until all of them are past the op's first claim
// check, so the invitations reach the claim together.
type racingAuth struct {
	*claimAuth
	arrived sync.WaitGroup
}

func (a *racingAuth) VerifyIssuer(sc *auth.SignedContract) error {
	a.arrived.Done()
	a.arrived.Wait()
	return secp256k1sig.VerifyASN1(secp256k1.FromIdentity(sc.Issuer), sc.SignableHash(), sc.IssuerSig)
}

func (a *racingAuth) IndexContract(*astral.Context, *auth.SignedContract) error { return nil }

// invitation is a node contract from a fresh user to nodeID with the user's
// signature.
func invitation(t *testing.T, nodeID *astral.Identity) (*auth.Contract, *crypto.Signature, *astral.Identity) {
	t.Helper()

	userKey := secp256k1.New()
	userID := secp256k1.Identity(secp256k1.PublicKey(userKey))
	contract, err := user.NewNodeContract(userID, nodeID, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	sig, err := secp256k1sig.SignASN1(userKey, (&auth.SignedContract{Contract: contract}).SignableHash())
	if err != nil {
		t.Fatalf("issuer sig: %v", err)
	}
	return contract, sig, userID
}

// routeInvitation sends one invitation to user.accept_membership, as astral-go's
// client does, and answers what the op wrote once it returned.
//
// why t.Errorf: it runs on a goroutine of its own, where t.Fatal is not allowed.
func routeInvitation(t *testing.T, mod *Module, nodeID *astral.Identity, contract *auth.Contract, sig *crypto.Signature) *answerWriter {
	op, err := routing.NewOp(mod.OpAcceptMembership)
	if err != nil {
		t.Errorf("new op: %v", err)
		return nil
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	w := &answerWriter{}
	q := query.New(astral.GenerateIdentity(), nodeID, user.OpAcceptMembership, nil)
	requester, err := op.RouteQuery(ctx, astral.Launch(q), w)
	if err != nil {
		t.Errorf("route: %v", err)
		return nil
	}
	defer requester.Close()
	sender := channel.NewSender(requester)
	if err = sender.Send(contract); err == nil {
		err = sender.Send(sig)
	}
	if err != nil {
		t.Errorf("send invitation: %v", err)
		return nil
	}

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Error("the op never returned")
	}
	return w
}

// Of invitations that all pass the first claim check, exactly one is answered
// with the subject signature and becomes the active contract; every other is
// refused with user.ErrAlreadyClaimed.
func TestAcceptMembershipConcurrentInvitationsClaimOnce(t *testing.T) {
	const n = 8

	f := newBootstrapFixture()
	ra := &racingAuth{claimAuth: f.auth}
	ra.arrived.Add(n)
	f.mod.Auth = ra
	f.mod.db = singleConnDB(t)
	f.mod.log = log.New(nil)
	f.mod.file.Policies.SwarmInvite = policyconf.Setting{Mode: policyconf.AcceptAll}

	users := make([]*astral.Identity, n)
	answers := make([]*answerWriter, n)
	var wg sync.WaitGroup
	for i := range n {
		contract, sig, userID := invitation(t, f.nodeID)
		users[i] = userID
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i] = routeInvitation(t, f.mod, f.nodeID, contract, sig)
		}()
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	var winner *astral.Identity
	for i, w := range answers {
		objs := w.objects(t)
		if len(objs) != 1 {
			t.Fatalf("invitation %d answered %v, want one object", i, objs)
		}
		switch a := objs[0].(type) {
		case *crypto.Signature:
			if winner != nil {
				t.Fatalf("invitations of %v and %v were both signed", winner, users[i])
			}
			winner = users[i]
		case astral.Error:
			if a.Error() != user.ErrAlreadyClaimed.Error() {
				t.Fatalf("invitation %d refused as %q, want %q", i, a, user.ErrAlreadyClaimed)
			}
		default:
			t.Fatalf("invitation %d answered %T", i, a)
		}
	}

	if winner == nil {
		t.Fatal("no invitation was signed")
	}
	if ac := f.mod.ActiveContract(); ac == nil || !ac.Issuer.IsEqual(winner) {
		t.Fatalf("the active contract is %v, want the signed invitation of %v", ac, winner)
	}
	if got := len(f.objects.objects()); got != 1 {
		t.Fatalf("%d contracts stored, want the winner's alone", got)
	}
}
