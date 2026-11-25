package user

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestSwarmViewDeclineBeatsLaterApproval is the working paper's "a Decline
// always wins": the offline Desktop declines R1 at 10:01, the Laptop approves
// it at 10:05 and the Pi signs App A's contract at 10:06. Once both answers
// have synced (11:00), R1 is declined on every node, in either merge order,
// although its contract stays stored.
func TestSwarmViewDeclineBeatsLaterApproval(t *testing.T) {
	for _, declineFirst := range []bool{true, false} {
		f := newViewFixture(t)
		r1 := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
		decline := f.decline(f.desktop, r1)
		approval := f.approval(f.laptop, r1)

		var approvalID *astral.ObjectID
		if declineFirst {
			f.add(decline)
			approvalID = f.add(approval)
		} else {
			approvalID = f.add(approval)
			f.now = viewClock(10, 6)
			f.wantStatus(r1, user.RequestApproved)
			f.add(f.appContract(f.pi, f.appA, approvalID))
			f.add(decline)
		}

		f.now = viewClock(11, 0)
		f.wantStatus(r1, user.RequestDeclined)
		if declineFirst {
			f.add(f.appContract(f.pi, f.appA, approvalID))
		}
		f.wantStatus(r1, user.RequestDeclined)
		if n := len(f.view.CarriedOut(approvalID)); n != 1 {
			t.Fatalf("CarriedOut = %d contracts, want the 10:06 contract", n)
		}
		// why: the Pi still sees the Approval and checks RequestStatus first.
		if !hasViewApproval(f.view.ApprovalsTouching(f.pi), approvalID) {
			t.Fatal("ApprovalsTouching(Pi) misses the since-declined Approval")
		}
		if f.view.ApprovedNotCarriedOut(r1) {
			t.Fatal("a declined request reported as approved, not yet carried out")
		}
	}
}

// TestSwarmViewDeclinedJoinCutsMembership applies the same rule to a join: a
// Decline after the carrier signed the membership makes that membership fail.
func TestSwarmViewDeclinedJoinCutsMembership(t *testing.T) {
	f := newViewFixture(t)
	sensor := astral.GenerateIdentity()
	r := f.add(f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0)))
	a := f.add(f.approval(f.laptop, r))
	f.add(f.member(f.phone, sensor, user.TierMember, a))
	f.wantMembership(sensor, true)

	f.add(f.decline(f.desktop, r))
	f.wantStatus(r, user.RequestDeclined)
	f.wantMembership(sensor, false)
}

// TestSwarmViewMemberApproval covers both sides of a Member and an Approval in
// the working paper: an Approval of a request a Member filed touches that
// Member, which acts on it; an Approval a Member signed carries no authority,
// so a membership resting on it does not hold.
func TestSwarmViewMemberApproval(t *testing.T) {
	t.Run("touches the Member that filed it", func(t *testing.T) {
		f := newViewFixture(t)
		r1 := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
		a := f.add(f.approval(f.laptop, r1))
		for _, c := range []struct {
			name string
			node *astral.Identity
			want bool
		}{
			{"filer Pi", f.pi, true},
			{"named App A", f.appA, true},
			{"approver Laptop", f.laptop, false},
			{"bystander Phone", f.phone, false},
		} {
			if got := hasViewApproval(f.view.ApprovalsTouching(c.node), a); got != c.want {
				t.Errorf("%s: touched = %v, want %v", c.name, got, c.want)
			}
		}
	})

	t.Run("approver tier", func(t *testing.T) {
		for _, c := range []struct {
			name     string
			approver func(f *viewFixture) *astral.Identity
			tier     user.Tier
			want     bool
		}{
			{"Member approves Member join", func(f *viewFixture) *astral.Identity { return f.pi }, user.TierMember, false},
			{"Trusted approves Member join", func(f *viewFixture) *astral.Identity { return f.phone }, user.TierMember, true},
			{"Privileged approves Member join", func(f *viewFixture) *astral.Identity { return f.laptop }, user.TierMember, true},
			{"user key approves Member join", func(f *viewFixture) *astral.Identity { return f.user }, user.TierMember, true},
			{"Trusted approves Trusted join", func(f *viewFixture) *astral.Identity { return f.phone }, user.TierTrusted, false},
			{"Privileged approves Trusted join", func(f *viewFixture) *astral.Identity { return f.laptop }, user.TierTrusted, true},
			{"carrier signs a Privileged membership", func(f *viewFixture) *astral.Identity { return f.user }, user.TierPrivilegedMember, false},
		} {
			t.Run(c.name, func(t *testing.T) {
				f := newViewFixture(t)
				joiner := astral.GenerateIdentity()
				r := f.add(f.joinRequest(joiner, f.laptop, c.tier, viewClock(10, 0)))
				a := f.add(f.approval(c.approver(f), r))
				f.add(f.member(f.laptop, joiner, c.tier, a))
				f.wantMembership(joiner, c.want)
			})
		}
	})
}

// TestSwarmViewExpelAfterApproval is "an expel beats everything signed by or
// for the expelled node": the Laptop approves the sensor's join, the Phone
// carries it out, and then the user key expels one of them.
func TestSwarmViewExpelAfterApproval(t *testing.T) {
	for _, c := range []struct {
		name       string
		expel      func(f *viewFixture, sensor *astral.Identity) *astral.Identity
		wantStatus user.RequestStatus
		wantMember bool
		wantTouch  bool
	}{
		{"nobody", func(f *viewFixture, s *astral.Identity) *astral.Identity { return nil }, user.RequestApproved, true, true},
		{"the joiner", func(f *viewFixture, s *astral.Identity) *astral.Identity { return s }, user.RequestDeclined, false, true},
		{"the carrier", func(f *viewFixture, s *astral.Identity) *astral.Identity { return f.phone }, user.RequestDeclined, false, true},
		{"the approver", func(f *viewFixture, s *astral.Identity) *astral.Identity { return f.laptop }, user.RequestPending, false, false},
		{"a bystander", func(f *viewFixture, s *astral.Identity) *astral.Identity { return f.pi }, user.RequestApproved, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newViewFixture(t)
			sensor := astral.GenerateIdentity()
			r := f.add(f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0)))
			a := f.add(f.approval(f.laptop, r))
			f.add(f.member(f.phone, sensor, user.TierMember, a))
			f.wantMembership(sensor, true)

			f.now = viewClock(10, 30)
			expelled := c.expel(f, sensor)
			if expelled != nil {
				f.add(f.expulsion(f.user, expelled))
				if !f.view.Expelled(expelled) {
					t.Fatal("Expelled = false after the Expulsion")
				}
			}
			f.wantStatus(r, c.wantStatus)
			f.wantMembership(sensor, c.wantMember)
			// note: only the approver's expel removes its Approval; the
			// carrier then reads the declined status before acting.
			if got := hasViewApproval(f.view.ApprovalsTouching(f.phone), a); got != c.wantTouch {
				t.Fatalf("ApprovalsTouching(carrier) has the Approval = %v, want %v", got, c.wantTouch)
			}
		})
	}
}

// TestSwarmViewExpelRequestCarriedOut: an ExpelRequest is approved by the root
// key's Expulsion of its Subject, which never counts as "not yet carried out".
func TestSwarmViewExpelRequestCarriedOut(t *testing.T) {
	f := newViewFixture(t)
	r := f.add(f.expelRequest(f.phone, f.pi, viewClock(10, 0)))
	f.wantStatus(r, user.RequestPending)
	f.add(f.expulsion(f.laptop, f.pi)) // not the root key
	f.wantStatus(r, user.RequestPending)
	f.add(f.expulsion(f.user, f.pi))
	f.wantStatus(r, user.RequestApproved)
	if f.view.ApprovedNotCarriedOut(r) {
		t.Fatal("an expel is reported as not yet carried out")
	}
	f.wantMembership(f.pi, false)
}

// TestSwarmViewResumedJoinRequest is "Resuming a join": the Pi signs R5 naming
// the Laptop as carrier at 10:00, the connection drops, the Desktop approves at
// 10:30, and the Pi resends the same object at 10:40 and again at 10:50.
func TestSwarmViewResumedJoinRequest(t *testing.T) {
	f := newViewFixture(t)
	sensor := astral.GenerateIdentity()
	r5obj := f.joinRequest(sensor, f.laptop, user.TierMember, viewClock(10, 0))
	r5 := f.add(r5obj)
	f.wantStatus(r5, user.RequestPending)

	f.now = viewClock(10, 30)
	a := f.add(f.approval(f.desktop, r5))
	head := f.state.SwarmHead()

	f.now = viewClock(10, 40)
	resent := resendJoin(t, r5obj)
	if id := f.add(resent); !id.IsEqual(r5) || f.state.SwarmHead() != head {
		t.Fatal("the resent JoinRequest is a new entry")
	}
	f.wantStatus(r5, user.RequestApproved)
	if len(f.view.CarriedOut(a)) != 0 || !f.view.ApprovedNotCarriedOut(r5) {
		t.Fatal("want approved, not yet carried out")
	}
	if !hasViewApproval(f.view.ApprovalsTouching(f.laptop), a) {
		t.Fatal("ApprovalsTouching(carrier) misses the Approval given while apart")
	}

	f.add(f.member(f.laptop, sensor, user.TierMember, a))
	f.wantMembership(sensor, true)
	if f.view.ApprovedNotCarriedOut(r5) {
		t.Fatal("still not carried out after the membership was stored")
	}

	// why: the carrier remembers nothing between connections; the stored
	// membership is what it hands over on the next resume.
	f.now = viewClock(10, 50)
	f.add(resendJoin(t, r5obj))
	if got := f.view.CarriedOut(a); len(got) != 1 || !got[0].Subject.IsEqual(sensor) {
		t.Fatalf("CarriedOut on the second resume = %d contracts, want the membership", len(got))
	}
}

// TestSwarmViewResumedJoinRequestExpires: with no answer, a resume within the
// hour stays pending, and one after it finds R5 expired.
func TestSwarmViewResumedJoinRequestExpires(t *testing.T) {
	f := newViewFixture(t)
	r5obj := f.joinRequest(astral.GenerateIdentity(), f.laptop, user.TierMember, viewClock(10, 0))
	r5 := f.add(r5obj)
	f.now = viewClock(10, 59)
	f.add(resendJoin(t, r5obj))
	f.wantStatus(r5, user.RequestPending)
	f.now = viewClock(11, 1)
	f.add(resendJoin(t, r5obj))
	f.wantStatus(r5, user.RequestExpired)
}

// resendJoin round-trips obj through its encoding, as a joiner reloads its signed
// JoinRequest from disk.
func resendJoin(t *testing.T, obj *user.SignedJoinRequest) *user.SignedJoinRequest {
	t.Helper()
	var buf bytes.Buffer
	if _, err := obj.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := &user.SignedJoinRequest{}
	if _, err := out.ReadFrom(&buf); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSwarmViewStatusPrecedence: declined > approved > expired > pending, with
// answers from expelled signers ignored.
func TestSwarmViewStatusPrecedence(t *testing.T) {
	type answers struct{ decline, approve, late, expelAnswerer bool }
	for _, c := range []struct {
		answers
		want user.RequestStatus
	}{
		{answers{}, user.RequestPending},
		{answers{late: true}, user.RequestExpired},
		{answers{approve: true}, user.RequestApproved},
		{answers{approve: true, late: true}, user.RequestApproved},
		{answers{decline: true}, user.RequestDeclined},
		{answers{decline: true, late: true}, user.RequestDeclined},
		{answers{decline: true, approve: true}, user.RequestDeclined},
		{answers{decline: true, approve: true, late: true}, user.RequestDeclined},
		{answers{approve: true, expelAnswerer: true}, user.RequestPending},
		{answers{decline: true, expelAnswerer: true}, user.RequestPending},
		{answers{decline: true, approve: true, late: true, expelAnswerer: true}, user.RequestExpired},
	} {
		f := newViewFixture(t)
		r := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
		if c.decline {
			f.add(f.decline(f.desktop, r))
		}
		if c.approve {
			f.add(f.approval(f.desktop, r))
		}
		if c.expelAnswerer {
			f.add(f.expulsion(f.user, f.desktop))
		}
		f.now = viewClock(10, 30)
		if c.late {
			f.now = viewClock(12, 0)
		}
		if got := f.view.RequestStatus(r); got != c.want {
			t.Errorf("%+v: RequestStatus = %v, want %v", c.answers, got, c.want)
		}
	}
}

// TestSwarmViewStatusParties: a request filed by, carried by or naming an
// expelled identity is declined; an unknown request is pending.
func TestSwarmViewStatusParties(t *testing.T) {
	sensor := astral.GenerateIdentity()
	for _, c := range []struct {
		name  string
		req   func(f *viewFixture) astral.Object
		expel func(f *viewFixture) *astral.Identity
	}{
		{"ContractRequest filer", func(f *viewFixture) astral.Object { return f.contractRequest(f.pi, f.appA, viewClock(10, 0)) }, func(f *viewFixture) *astral.Identity { return f.pi }},
		{"ContractRequest identity", func(f *viewFixture) astral.Object { return f.contractRequest(f.pi, f.appA, viewClock(10, 0)) }, func(f *viewFixture) *astral.Identity { return f.appA }},
		{"JoinRequest carrier", func(f *viewFixture) astral.Object {
			return f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0))
		}, func(f *viewFixture) *astral.Identity { return f.phone }},
		{"JoinRequest joiner", func(f *viewFixture) astral.Object {
			return f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0))
		}, func(*viewFixture) *astral.Identity { return sensor }},
		{"ExpelRequest filer", func(f *viewFixture) astral.Object { return f.expelRequest(f.phone, f.pi, viewClock(10, 0)) }, func(f *viewFixture) *astral.Identity { return f.phone }},
	} {
		f := newViewFixture(t)
		r := f.add(c.req(f))
		f.add(f.approval(f.laptop, r))
		f.add(f.expulsion(f.user, c.expel(f)))
		if got := f.view.RequestStatus(r); got != user.RequestDeclined {
			t.Errorf("%s expelled: RequestStatus = %v, want declined", c.name, got)
		}
	}

	f := newViewFixture(t)
	unknown, _ := astral.ResolveObjectID(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	f.wantStatus(unknown, user.RequestPending)
	f.wantStatus(nil, user.RequestPending)
}

// TestSwarmViewExpiryBoundary: a request is answerable up to and including its
// Timeout, for the 1 h default and a shorter Timeout, and expired 1 ns after.
func TestSwarmViewExpiryBoundary(t *testing.T) {
	for _, timeout := range []time.Duration{user.RequestTimeout, 10 * time.Minute} {
		for _, c := range []struct {
			offset time.Duration
			want   user.RequestStatus
		}{
			{-time.Nanosecond, user.RequestPending},
			{0, user.RequestPending},
			{time.Nanosecond, user.RequestExpired},
		} {
			f := newViewFixture(t)
			req := f.contractRequest(f.pi, f.appA, viewClock(10, 0))
			req.Timeout = astral.Time(viewClock(10, 0).Add(timeout))
			r := f.add(req)
			f.now = viewClock(10, 0).Add(timeout + c.offset)
			if got := f.view.RequestStatus(r); got != c.want {
				t.Errorf("timeout %v, offset %v: RequestStatus = %v, want %v", timeout, c.offset, got, c.want)
			}
			if got := req.Expired(f.now); got != (c.want == user.RequestExpired) {
				t.Errorf("timeout %v, offset %v: the view disagrees with Expired = %v", timeout, c.offset, got)
			}
		}
	}
}

// TestSwarmViewMembershipValidity: Membership skips expired and unsigned
// contracts and picks the one that expires last.
func TestSwarmViewMembershipValidity(t *testing.T) {
	f := newViewFixture(t)
	node := astral.GenerateIdentity()
	f.add(f.memberUntil(f.user, node, user.TierMember, nil, viewClock(11, 0)))
	f.add(f.memberUntil(f.user, node, user.TierTrusted, nil, viewClock(12, 0)))
	unsigned := f.memberUntil(f.user, node, user.TierPrivilegedMember, nil, viewClock(13, 0))
	unsigned.IssuerSig = nil
	f.add(unsigned)

	f.now = viewClock(10, 30)
	if m, ok := f.view.Membership(node); !ok || m.Tier != user.TierTrusted {
		t.Fatalf("Membership = %v, want the trusted one expiring last", m)
	}
	f.now = viewClock(12, 0)
	f.wantMembership(node, false)
}

// TestSwarmViewRereadsOnlyOnHeadChange: the view reads the set again only when
// SwarmHead has grown.
func TestSwarmViewRereadsOnlyOnHeadChange(t *testing.T) {
	f := newViewFixture(t)
	r := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	f.wantStatus(r, user.RequestPending)
	reads := f.state.reads
	f.wantStatus(r, user.RequestPending)
	if f.state.reads != reads {
		t.Fatal("the set was read again with an unchanged head")
	}
	f.add(f.approval(f.laptop, r))
	f.wantStatus(r, user.RequestApproved)
}
