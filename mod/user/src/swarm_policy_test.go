package user

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/user"
)

func roundTrip[T astral.Object](t *testing.T, obj T) T {
	t.Helper()
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, obj); err != nil {
		t.Fatal(err)
	}
	got, err := astral.DecodeAs[T](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The requests and decisions cross to the delegate as objects, so each must
// come back from the wire unchanged.
func TestSwarmPolicyObjectsSurviveTheWire(t *testing.T) {
	requester := astral.GenerateIdentity()
	inviter := astral.GenerateIdentity()

	join := roundTrip(t, &user.SwarmJoinRequest{Requester: requester})
	if !join.Requester.IsEqual(requester) {
		t.Fatalf("join requester changed: %v", join.Requester)
	}

	if !roundTrip(t, &user.SwarmJoinDecision{Allow: true}).Allow {
		t.Fatal("join decision lost Allow")
	}

	contract := &auth.Contract{Issuer: inviter, Subject: requester}
	invite := roundTrip(t, &user.SwarmInviteRequest{Inviter: inviter, Contract: contract})
	if !invite.Inviter.IsEqual(inviter) || invite.Contract == nil || !invite.Contract.Subject.IsEqual(requester) {
		t.Fatalf("invite request changed: %+v", invite)
	}

	if roundTrip(t, &user.SwarmInviteDecision{Allow: false}).Allow {
		t.Fatal("invite decision gained Allow")
	}
}

// With no delegate set, both swarm policies keep today's accept-all behavior.
func TestNoSwarmDelegateKeepsAcceptAll(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := astral.GenerateIdentity()

	if !mod.GetSwarmJoinRequestPolicy()(nil, id) {
		t.Fatal("join refused with no delegate set")
	}
	if !mod.GetSwarmInvitePolicy()(nil, id, &auth.Contract{Subject: id}) {
		t.Fatal("invite refused with no delegate set")
	}
}

// A delegated policy reached without a delegate refuses rather than admits.
func TestDelegatedSwarmPolicyWithoutDelegateRefuses(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := astral.GenerateIdentity()

	if mod.SwarmJoinViaDelegate(nil, id) {
		t.Fatal("join admitted with no delegate")
	}
	if mod.SwarmInviteViaDelegate(nil, id, &auth.Contract{Subject: id}) {
		t.Fatal("invite admitted with no delegate")
	}
}
