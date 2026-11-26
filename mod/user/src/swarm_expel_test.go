package user

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// Done: an expel needs an ExpelRequest from a Trusted or Privileged member and
// the user key's Expulsion.
func TestExpelNeedsRequestAndUserKey(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	ctx := astral.NewContext(nil)
	subject := f.keys.newKey()
	f.swarm.setTier(f.userID, subject, user.TierMember)
	ceremony := newUserKeyCeremony(f.userID, f.keys, f.swarm, f.swarm)
	ceremony.now = f.clock.now

	// no request, no expel
	unstoredID, _ := astral.ResolveObjectID(signedExpelRequest(t, f, f.carrier, subject))
	if _, err := ceremony.Expel(ctx, unstoredID); !errors.Is(err, errNoExpelRequest) {
		t.Fatalf("expel without a request: %v, want %v", err, errNoExpelRequest)
	}

	// a request filed by a Member is refused
	memberReq := signedExpelRequest(t, f, subject, f.carrier)
	memberReqID := addSwarmEntryT(t, f, memberReq)
	if _, err := ceremony.Expel(ctx, memberReqID); !errors.Is(err, errExpelFilerTier) {
		t.Fatalf("expel on a Member's request: %v, want %v", err, errExpelFilerTier)
	}

	// a Trusted node files; a ceremony without the user key signs nothing
	req, err := f.newWriter().FileExpelRequest(ctx, subject)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	reqID, _ := astral.ResolveObjectID(req)
	noKey := newUserKeyCeremony(astral.GenerateIdentity(), f.keys, f.swarm, f.swarm)
	if _, err = noKey.Expel(ctx, reqID); err == nil {
		t.Fatal("expelled without the user key")
	}
	if f.swarm.Expelled(subject) {
		t.Fatal("subject expelled without the user key")
	}

	// the user key signs the Expulsion
	expulsion, err := ceremony.Expel(ctx, reqID)
	if err != nil {
		t.Fatalf("expel: %v", err)
	}
	if !expulsion.Issuer.IsEqual(f.userID) || !expulsion.Subject.IsEqual(subject) {
		t.Fatalf("expulsion: %v expels %v", expulsion.Issuer, expulsion.Subject)
	}
	verifySignedBy(t, f.userID, expulsion.Expulsion, expulsion.IssuerSig)
	if !f.swarm.Expelled(subject) || f.swarm.RequestStatus(reqID) != user.RequestApproved {
		t.Fatal("expulsion did not carry out the request")
	}
	if text := f.keys.texts[len(f.keys.texts)-1]; !strings.Contains(text, "expels") {
		t.Fatalf("ceremony text %q", text)
	}
}

// An expired or declined ExpelRequest is not carried out.
func TestExpelRefusesEndedRequest(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	ctx := astral.NewContext(nil)
	ceremony := newUserKeyCeremony(f.userID, f.keys, f.swarm, f.swarm)
	ceremony.now = f.clock.now

	declined, err := f.newWriter().FileExpelRequest(ctx, f.keys.newKey())
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	declinedID, _ := astral.ResolveObjectID(declined)
	answerJoin(t, f.swarm, f.keys, f.userID, declinedID, true)
	if _, err = ceremony.Expel(ctx, declinedID); !errors.Is(err, errExpelDeclined) {
		t.Fatalf("expel on a declined request: %v, want %v", err, errExpelDeclined)
	}

	expired := signedExpelRequest(t, f, f.carrier, f.keys.newKey())
	expiredID := addSwarmEntryT(t, f, expired)
	ceremony.now = func() time.Time { return f.clock.now().Add(2 * user.RequestTimeout) }
	if _, err = ceremony.Expel(ctx, expiredID); !errors.Is(err, errExpelExpired) {
		t.Fatalf("expel on an expired request: %v, want %v", err, errExpelExpired)
	}
}

func signedExpelRequest(t *testing.T, f *carrierFixture, filedBy, subject *astral.Identity) *user.SignedExpelRequest {
	t.Helper()
	req := user.NewExpelRequest(filedBy, subject)
	sig, err := secp256k1sig.SignASN1(f.keys.key(filedBy), req.SignableHash())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return &user.SignedExpelRequest{ExpelRequest: req, FiledBySig: sig}
}

func addSwarmEntryT(t *testing.T, f *carrierFixture, obj astral.Object) *astral.ObjectID {
	t.Helper()
	if err := f.swarm.merge(nil, obj); err != nil {
		t.Fatalf("add: %v", err)
	}
	id, _ := astral.ResolveObjectID(obj)
	return id
}
