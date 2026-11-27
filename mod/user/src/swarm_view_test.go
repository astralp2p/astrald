package user

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestSwarmViewDeclineClosesOnlyTheRequest: the offline Desktop declines R1
// at 10:01, the Laptop offers for it at 10:05, and the Pi co-signs the offer
// and signs App A's contract at 10:06. A Decline closes the request but takes
// back no contract both parties signed (PR 4 decision Q1), so once both
// answers have synced R1 is fulfilled on every node, in either merge order;
// without the contract it is declined.
//
// note: ported from the Approval at 10:05; a contract request is answered by
// an offer since the two-signature model.
func TestSwarmViewDeclineClosesOnlyTheRequest(t *testing.T) {
	for _, declineFirst := range []bool{true, false} {
		f := newViewFixture(t)
		r1obj := f.contractRequest(f.pi, f.appA, viewClock(10, 0))
		r1 := f.add(r1obj)
		decline := f.decline(f.desktop, r1)
		offer := f.offer(f.laptop, r1obj, r1)

		if declineFirst {
			f.add(decline)
			f.add(offer)
			f.wantStatus(r1, user.RequestDeclined)
		} else {
			f.add(offer)
			f.now = viewClock(10, 6)
			f.wantStatus(r1, user.RequestOffered)
		}
		f.add(complete(offer))
		contract := f.appContract(f.pi, f.appA, r1)
		f.add(contract)
		f.add(decline)

		f.now = viewClock(11, 0)
		f.wantStatus(r1, user.RequestFulfilled)
		if !f.view.snapshot().ContractCounts(contract) {
			t.Fatal("the contract signed by both stopped counting after the Decline")
		}
		if n := len(f.view.ContractsOf(r1)); n != 3 {
			t.Fatalf("ContractsOf = %d contracts, want the offer, its completed form and the app's contract", n)
		}
	}
}

// TestSwarmViewDeclineKeepsASignedMembership applies the same rule to a join:
// a Decline after the carrier and the joiner signed the membership leaves it
// holding.
func TestSwarmViewDeclineKeepsASignedMembership(t *testing.T) {
	f := newViewFixture(t)
	sensor := astral.GenerateIdentity()
	r := f.add(f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0)))
	f.add(f.member(f.phone, sensor, user.TierMember, r))
	f.wantMembership(sensor, true)

	f.add(f.decline(f.desktop, r))
	f.wantStatus(r, user.RequestFulfilled)
	f.wantMembership(sensor, true)
}

// TestSwarmViewMemberApproval: an Approval of a request a Member filed touches
// that Member, which acts on it.
func TestSwarmViewMemberApproval(t *testing.T) {
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
}

// TestSwarmViewIssuerTierBoundsTheMembership: a membership holds only when its
// issuer may issue its tier (swarmset.MayIssue): the user key any tier, a
// Privileged member Member and Trusted, a Trusted member Member, a Member none.
// Its RequestID and any Approval add nothing.
//
// note: ported from the Approval-link rows ("approver tier", "link",
// "ContractRequest Approval"); a membership rests on no Approval since the
// two-signature model, so the rows now vary the issuer.
func TestSwarmViewIssuerTierBoundsTheMembership(t *testing.T) {
	for _, c := range []struct {
		name   string
		issuer func(f *viewFixture) *astral.Identity
		tier   user.Tier
		want   bool
	}{
		{"Member issues Member", func(f *viewFixture) *astral.Identity { return f.pi }, user.TierMember, false},
		{"Trusted issues Member", func(f *viewFixture) *astral.Identity { return f.phone }, user.TierMember, true},
		{"Trusted issues Trusted", func(f *viewFixture) *astral.Identity { return f.phone }, user.TierTrusted, false},
		{"Privileged issues Member", func(f *viewFixture) *astral.Identity { return f.laptop }, user.TierMember, true},
		{"Privileged issues Trusted", func(f *viewFixture) *astral.Identity { return f.laptop }, user.TierTrusted, true},
		{"Privileged issues Privileged", func(f *viewFixture) *astral.Identity { return f.laptop }, user.TierPrivilegedMember, false},
		{"user key issues Privileged", func(f *viewFixture) *astral.Identity { return f.user }, user.TierPrivilegedMember, true},
		{"stranger issues Member", func(*viewFixture) *astral.Identity { return astral.GenerateIdentity() }, user.TierMember, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newViewFixture(t)
			joiner := astral.GenerateIdentity()
			r := f.add(f.joinRequest(joiner, f.laptop, c.tier, viewClock(10, 0)))
			f.add(f.approval(f.user, r))
			f.add(f.member(c.issuer(f), joiner, c.tier, r))
			f.wantMembership(joiner, c.want)
		})
	}
}

// TestSwarmViewContractsOfMatchesRequest: ContractsOf returns only the
// contracts that name the given request, not those of another one.
func TestSwarmViewContractsOfMatchesRequest(t *testing.T) {
	f := newViewFixture(t)
	appB := astral.GenerateIdentity()
	r1 := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	r2 := f.add(f.contractRequest(f.pi, appB, viewClock(10, 0)))
	f.add(f.appContract(f.pi, f.appA, r1))
	f.add(f.appContract(f.pi, appB, r2))
	f.add(f.appContract(f.pi, appB, nil))
	for _, c := range []struct {
		req     *astral.ObjectID
		subject *astral.Identity
	}{{r1, f.appA}, {r2, appB}} {
		got := f.view.ContractsOf(c.req)
		if len(got) != 1 || !got[0].Subject.IsEqual(c.subject) || !got[0].RequestID.IsEqual(c.req) {
			t.Fatalf("ContractsOf = %d contracts, want only the one naming this request", len(got))
		}
	}
}

// TestSwarmViewExpelRequestTouchesSubject: an Approval of an ExpelRequest
// touches its filer and its Subject, and no bystander.
func TestSwarmViewExpelRequestTouchesSubject(t *testing.T) {
	f := newViewFixture(t)
	r := f.add(f.expelRequest(f.phone, f.pi, viewClock(10, 0)))
	a := f.add(f.approval(f.laptop, r))
	for _, c := range []struct {
		name string
		node *astral.Identity
		want bool
	}{
		{"filer Phone", f.phone, true},
		{"subject Pi", f.pi, true},
		{"bystander Desktop", f.desktop, false},
	} {
		if got := hasViewApproval(f.view.ApprovalsTouching(c.node), a); got != c.want {
			t.Errorf("%s: touched = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSwarmViewReadErrorKeepsHeldEntries: the set only grows, so a failed
// read after the head grew answers from what an earlier read held as well;
// an expel or a Decline is never undone by a read error.
func TestSwarmViewReadErrorKeepsHeldEntries(t *testing.T) {
	for _, c := range []struct {
		name string
		fail func(s *fakeSwarmState)
	}{
		{"listing the expels", func(s *fakeSwarmState) { s.failList = user.EntryExpels }},
		{"reading an expel", func(s *fakeSwarmState) { s.failRead = user.EntryExpels }},
		{"listing the declines", func(s *fakeSwarmState) { s.failList = user.EntryDeclines }},
		{"reading a decline", func(s *fakeSwarmState) { s.failRead = user.EntryDeclines }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newViewFixture(t)
			robj := f.contractRequest(f.pi, f.appA, viewClock(10, 0))
			r := f.add(robj)
			f.add(f.offer(f.laptop, robj, r))
			f.add(f.decline(f.desktop, r))
			f.add(f.expulsion(f.user, f.phone))
			f.wantMembership(f.phone, false)
			f.wantStatus(r, user.RequestDeclined)

			c.fail(f.state)
			r2obj := f.contractRequest(f.pi, f.appA, viewClock(10, 1))
			r2 := f.add(r2obj)
			f.wantMembership(f.phone, false)
			f.wantStatus(r, user.RequestDeclined)
			// why: what the failed read did return is answered from too.
			if !f.view.Expelled(f.phone) || f.view.RequestStatus(r2) != user.RequestPending {
				t.Fatal("the failed read lost the held set")
			}
			f.add(f.offer(f.laptop, r2obj, r2))
			f.wantStatus(r2, user.RequestOffered)

			// why: after a failed read the head is not taken as read, so
			// the next call reads again with no new entry.
			f.state.failList, f.state.failRead = "", ""
			reads := f.state.reads
			f.wantMembership(f.phone, false)
			if f.state.reads == reads {
				t.Fatal("the set was not read again after a failed read")
			}
			reads = f.state.reads
			f.wantStatus(r, user.RequestDeclined)
			if f.state.reads != reads {
				t.Fatal("the set was read again after a good read with an unchanged head")
			}
		})
	}
}

// TestSwarmViewExpelCutsAMembership is "an expel beats everything signed by or
// for the expelled node": the Phone signs the sensor's membership, and then
// the user key expels one of them. The request stays fulfilled.
func TestSwarmViewExpelCutsAMembership(t *testing.T) {
	for _, c := range []struct {
		name       string
		expel      func(f *viewFixture, sensor *astral.Identity) *astral.Identity
		wantMember bool
	}{
		{"nobody", func(f *viewFixture, s *astral.Identity) *astral.Identity { return nil }, true},
		{"the joiner", func(f *viewFixture, s *astral.Identity) *astral.Identity { return s }, false},
		{"the carrier", func(f *viewFixture, s *astral.Identity) *astral.Identity { return f.phone }, false},
		{"a bystander", func(f *viewFixture, s *astral.Identity) *astral.Identity { return f.pi }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newViewFixture(t)
			sensor := astral.GenerateIdentity()
			r := f.add(f.joinRequest(sensor, f.phone, user.TierMember, viewClock(10, 0)))
			f.add(f.member(f.phone, sensor, user.TierMember, r))
			f.wantMembership(sensor, true)

			f.now = viewClock(10, 30)
			if expelled := c.expel(f, sensor); expelled != nil {
				f.add(f.expulsion(f.user, expelled))
				if !f.view.Expelled(expelled) {
					t.Fatal("Expelled = false after the Expulsion")
				}
			}
			f.wantStatus(r, user.RequestFulfilled)
			f.wantMembership(sensor, c.wantMember)
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
// the Laptop as carrier at 10:00, the connection drops, and the Pi resends the
// same object at 10:40 and, after the membership was stored, at 10:50.
func TestSwarmViewResumedJoinRequest(t *testing.T) {
	f := newViewFixture(t)
	sensor := astral.GenerateIdentity()
	r5obj := f.joinRequest(sensor, f.laptop, user.TierMember, viewClock(10, 0))
	r5 := f.add(r5obj)
	f.wantStatus(r5, user.RequestPending)
	head := f.state.SwarmHead()

	f.now = viewClock(10, 40)
	resent := resendJoin(t, r5obj)
	if id := f.add(resent); !id.IsEqual(r5) || f.state.SwarmHead() != head {
		t.Fatal("the resent JoinRequest is a new entry")
	}
	f.wantStatus(r5, user.RequestPending)
	if len(f.view.ContractsOf(r5)) != 0 {
		t.Fatal("want no membership yet")
	}

	f.add(f.member(f.laptop, sensor, user.TierMember, r5))
	f.wantMembership(sensor, true)
	f.wantStatus(r5, user.RequestFulfilled)

	// why: the carrier remembers nothing between connections; the stored
	// membership is what it hands over on the next resume.
	f.now = viewClock(10, 50)
	f.add(resendJoin(t, r5obj))
	if got := f.view.ContractsOf(r5); len(got) != 1 || !got[0].Subject.IsEqual(sensor) {
		t.Fatalf("ContractsOf on the second resume = %d contracts, want the membership", len(got))
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

// TestSwarmViewStatusPrecedence: fulfilled > declined > approved > expired >
// offered > pending, with answers from expelled signers ignored. Only an
// ExpelRequest is approved; a ContractRequest is offered.
//
// note: the rows that approved a ContractRequest now offer for it; the
// approved rows moved to an ExpelRequest.
func TestSwarmViewStatusPrecedence(t *testing.T) {
	type answers struct{ fulfil, decline, approve, late, expelAnswerer bool }
	for _, c := range []struct {
		answers
		want, wantExpel user.RequestStatus
	}{
		{answers{}, user.RequestPending, user.RequestPending},
		{answers{late: true}, user.RequestExpired, user.RequestExpired},
		{answers{approve: true}, user.RequestOffered, user.RequestApproved},
		{answers{approve: true, late: true}, user.RequestExpired, user.RequestApproved},
		{answers{decline: true}, user.RequestDeclined, user.RequestDeclined},
		{answers{decline: true, late: true}, user.RequestDeclined, user.RequestDeclined},
		{answers{decline: true, approve: true}, user.RequestDeclined, user.RequestDeclined},
		{answers{decline: true, approve: true, late: true}, user.RequestDeclined, user.RequestDeclined},
		{answers{approve: true, expelAnswerer: true}, user.RequestPending, user.RequestPending},
		{answers{decline: true, expelAnswerer: true}, user.RequestPending, user.RequestPending},
		{answers{decline: true, approve: true, late: true, expelAnswerer: true}, user.RequestExpired, user.RequestExpired},
		{answers{fulfil: true}, user.RequestFulfilled, ""},
		{answers{fulfil: true, decline: true, approve: true, late: true}, user.RequestFulfilled, ""},
	} {
		for _, expel := range []bool{false, true} {
			want := c.want
			if expel {
				if want = c.wantExpel; want == "" {
					continue // why: an ExpelRequest is never fulfilled
				}
			}
			if got := precedenceStatus(t, c.answers, expel); got != want {
				t.Errorf("%+v, expel request %v: RequestStatus = %v, want %v", c.answers, expel, got, want)
			}
		}
	}
}

// precedenceStatus answers a ContractRequest, or an ExpelRequest, as a says and
// returns its status.
func precedenceStatus(t *testing.T, a struct{ fulfil, decline, approve, late, expelAnswerer bool }, expel bool) user.RequestStatus {
	f := newViewFixture(t)
	cr := f.contractRequest(f.pi, f.appA, viewClock(10, 0))
	var r *astral.ObjectID
	if expel {
		r = f.add(f.expelRequest(f.phone, f.appA, viewClock(10, 0)))
	} else {
		r = f.add(cr)
	}
	if a.fulfil {
		f.add(f.appContract(f.pi, f.appA, r))
	}
	if a.decline {
		f.add(f.decline(f.desktop, r))
	}
	switch {
	case a.approve && expel:
		f.add(f.approval(f.desktop, r))
	case a.approve:
		f.add(f.offer(f.desktop, cr, r))
	}
	if a.expelAnswerer {
		f.add(f.expulsion(f.user, f.desktop))
	}
	f.now = viewClock(10, 30)
	if a.late {
		f.now = viewClock(12, 0)
	}
	return f.view.RequestStatus(r)
}

// TestSwarmViewOfferedNeedsALiveOffer: a request is offered only while an
// offer from a non-expelled issuer that has not expired names it; an Approval
// of a ContractRequest is no answer.
func TestSwarmViewOfferedNeedsALiveOffer(t *testing.T) {
	f := newViewFixture(t)
	robj := f.contractRequest(f.pi, f.appA, viewClock(10, 0))
	r := f.add(robj)
	f.add(f.approval(f.laptop, r))
	f.wantStatus(r, user.RequestPending)

	short := f.offer(f.desktop, robj, r)
	short.ExpiresAt = astral.Time(viewClock(10, 20))
	f.add(short)
	f.wantStatus(r, user.RequestOffered)
	f.now = viewClock(10, 20)
	f.wantStatus(r, user.RequestPending)

	f.add(f.offer(f.laptop, robj, r))
	f.wantStatus(r, user.RequestOffered)
	f.add(f.expulsion(f.user, f.laptop))
	f.wantStatus(r, user.RequestPending)
}

// TestSwarmViewFulfilledNeedsBothSignaturesAndAParty: a contract that names
// a request fulfils it only with both signatures, and only when it is given
// to the identity the request names or to its filer. A half-signed contract to
// the app is not an offer, so the request stays pending.
func TestSwarmViewFulfilledNeedsBothSignaturesAndAParty(t *testing.T) {
	f := newViewFixture(t)
	r := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	half := f.appContract(f.pi, f.appA, r)
	half.SubjectSig = nil
	f.add(half)
	f.add(f.appContract(f.pi, f.phone, r))
	f.wantStatus(r, user.RequestPending)

	f.add(f.appContract(f.laptop, f.pi, r))
	f.wantStatus(r, user.RequestFulfilled)
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
// contracts.
func TestSwarmViewMembershipValidity(t *testing.T) {
	f := newViewFixture(t)
	node := astral.GenerateIdentity()
	f.add(f.memberUntil(f.user, node, user.TierMember, nil, viewClock(11, 0)))
	unsigned := f.memberUntil(f.user, node, user.TierPrivilegedMember, nil, viewClock(13, 0))
	unsigned.IssuerSig = nil
	f.add(unsigned)

	f.now = viewClock(10, 30)
	if m, ok := f.view.Membership(node); !ok || m.Tier != user.TierMember {
		t.Fatalf("Membership = %v, want the signed member one", m)
	}
	f.now = viewClock(11, 0)
	f.wantMembership(node, false)
}

// TestSwarmViewMembershipPicksTheHighestTierFirst: of a node's memberships the
// view reads the highest tier, and among those the one that expires last, so
// a lower tier that expires later does not hide a higher one.
func TestSwarmViewMembershipPicksTheHighestTierFirst(t *testing.T) {
	f := newViewFixture(t)
	node := astral.GenerateIdentity()
	f.add(f.memberUntil(f.user, node, user.TierMember, nil, viewClock(14, 0)))
	f.add(f.memberUntil(f.laptop, node, user.TierTrusted, nil, viewClock(11, 0)))
	f.add(f.memberUntil(f.user, node, user.TierTrusted, nil, viewClock(12, 0)))

	for _, c := range []struct {
		at      time.Time
		tier    user.Tier
		expires time.Time
	}{
		{viewClock(10, 30), user.TierTrusted, viewClock(12, 0)},
		{viewClock(11, 30), user.TierTrusted, viewClock(12, 0)},
		{viewClock(12, 30), user.TierMember, viewClock(14, 0)},
	} {
		f.now = c.at
		m, ok := f.view.Membership(node)
		if !ok || m.Tier != c.tier || !m.ExpiresAt.Time().Equal(c.expires) {
			t.Fatalf("at %v: Membership = %v, want %v until %v", c.at.Format("15:04"), m, c.tier, c.expires.Format("15:04"))
		}
	}
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
	f.add(f.decline(f.laptop, r))
	f.wantStatus(r, user.RequestDeclined)
}

// TestSwarmViewMembershipHoldsOnlyTheTier: a carrier's membership answering a
// JoinRequest holds only when its permits are the tier permit
// alone; one with an extra permit, or a delegable tier permit, does not.
func TestSwarmViewMembershipHoldsOnlyTheTier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(c *auth.Contract)
		holds bool
	}{
		{"the tier alone", func(*auth.Contract) {}, true},
		{"an extra relay_for", func(c *auth.Contract) {
			c.Permits = append(c.Permits, &auth.Permit{Action: "mod.nodes.relay_for_action"})
		}, false},
		{"an extra admin_swarm", func(c *auth.Contract) {
			c.Permits = append(c.Permits, &auth.Permit{Action: "mod.user.admin_swarm_action"})
		}, false},
		{"a delegable tier", func(c *auth.Contract) { c.Permits[0].Delegation = 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newViewFixture(t)
			joiner := astral.GenerateIdentity()
			r := f.add(f.joinRequest(joiner, f.phone, user.TierMember, viewClock(10, 0)))
			m := f.member(f.phone, joiner, user.TierMember, r)
			tc.edit(m.Contract)
			f.add(m)
			f.wantMembership(joiner, tc.holds)
		})
	}
}

// TestRootKeyIsTheUserOrTheUnclaimedNode: the root key is the node itself
// while it is unclaimed, and the user once claimed.
func TestRootKeyIsTheUserOrTheUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}
	if !mod.rootKey().IsEqual(nodeID) {
		t.Fatal("unclaimed: want the node as root key")
	}

	userID := astral.GenerateIdentity()
	claimed := claimedModule(t, userID)
	claimed.node = &identityNode{id: nodeID}
	if !claimed.rootKey().IsEqual(userID) {
		t.Fatal("claimed: want the user as root key")
	}
}

// TestSwarmViewMembershipNeedsTheSubjectsSignature: a membership only its
// issuer signed makes no one a member and counts as no contract, at any tier
// and from any issuer, the root key included; the same membership signed by
// both does.
func TestSwarmViewMembershipNeedsTheSubjectsSignature(t *testing.T) {
	for _, c := range []struct {
		name   string
		issuer func(f *viewFixture) *astral.Identity
		tier   user.Tier
	}{
		{"the user key's privileged membership", func(f *viewFixture) *astral.Identity { return f.user }, user.TierPrivilegedMember},
		{"a carrier's member membership", func(f *viewFixture) *astral.Identity { return f.phone }, user.TierMember},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newViewFixture(t)
			node := astral.GenerateIdentity()
			half := f.member(c.issuer(f), node, c.tier, nil)
			half.SubjectSig = nil
			f.add(half)
			f.wantMembership(node, false)
			if f.view.snapshot().ContractCounts(half) {
				t.Fatal("a membership only its issuer signed counts")
			}

			f.add(f.member(c.issuer(f), node, c.tier, nil))
			f.wantMembership(node, true)
		})
	}
}
