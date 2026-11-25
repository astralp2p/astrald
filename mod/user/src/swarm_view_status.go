package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// requestParties are the identities a request involves and its Timeout.
type requestParties struct {
	filer   *astral.Identity // ContractRequest.FiledBy, ExpelRequest.FiledBy
	carrier *astral.Identity // JoinRequest.Carrier
	named   *astral.Identity // ContractRequest.Identity, JoinRequest.Joiner
	subject *astral.Identity // ExpelRequest.Subject
	timeout astral.Time
}

// partiesOf returns the parties of a stored request; ok is false for any
// other object.
func partiesOf(req astral.Object) (p requestParties, ok bool) {
	switch r := req.(type) {
	case *user.SignedContractRequest:
		return requestParties{filer: r.FiledBy, named: r.Identity, timeout: r.Timeout}, !r.IsNil()
	case *user.SignedJoinRequest:
		return requestParties{carrier: r.Carrier, named: r.Joiner, timeout: r.Timeout}, !r.IsNil()
	case *user.SignedExpelRequest:
		return requestParties{filer: r.FiledBy, subject: r.Subject, timeout: r.Timeout}, !r.IsNil()
	}
	return p, false
}

// touches reports whether node filed, carried in or is named by the request.
func (p requestParties) touches(node *astral.Identity) bool {
	for _, id := range []*astral.Identity{p.filer, p.carrier, p.named, p.subject} {
		if !id.IsZero() && id.IsEqual(node) {
			return true
		}
	}
	return false
}

// request returns the stored request with entry ID req and its parties.
func (s *swarmSnapshot) request(req *astral.ObjectID) (astral.Object, requestParties, bool) {
	if req == nil {
		return nil, requestParties{}, false
	}
	r, found := s.requests[*req]
	if !found {
		return nil, requestParties{}, false
	}
	p, ok := partiesOf(r)
	return r, p, ok
}

// requestStatus returns the status of req: declined > approved > expired >
// pending. An unknown req is pending.
func (s *swarmSnapshot) requestStatus(req *astral.ObjectID) user.RequestStatus {
	r, p, ok := s.request(req)
	switch {
	case !ok:
		return user.RequestPending
	case s.declined(req, p):
		return user.RequestDeclined
	case len(s.approvalsOf(req)) > 0 || s.expelCarriesOut(r, p):
		return user.RequestApproved
	case s.now.After(p.timeout.Time()):
		// why: the same boundary as the request bodies' Expired: at the
		// Timeout itself the request is still answerable.
		return user.RequestExpired
	}
	return user.RequestPending
}

// declined reports whether a counting Decline names req, or the request was
// filed by, carried by or names an expelled identity. A Decline wins even
// after a contract carried out an Approval of req.
func (s *swarmSnapshot) declined(req *astral.ObjectID, p requestParties) bool {
	if s.expelled(p.filer) || s.expelled(p.carrier) || s.expelled(p.named) {
		return true
	}
	for _, d := range s.declines {
		if !d.IsNil() && d.RequestID != nil && d.RequestID.IsEqual(req) && s.counts(d.Issuer) {
			return true
		}
	}
	return false
}

// approvalsOf returns the counting Approvals of req, whenever they were given.
func (s *swarmSnapshot) approvalsOf(req *astral.ObjectID) []*user.SignedApproval {
	var list []*user.SignedApproval
	for _, a := range s.approvals {
		if s.countingApproval(a, req) {
			list = append(list, a)
		}
	}
	return list
}

// countingApproval reports whether a is an Approval of req whose signer is not
// expelled.
func (s *swarmSnapshot) countingApproval(a *user.SignedApproval, req *astral.ObjectID) bool {
	return !a.IsNil() && a.RequestID != nil && a.RequestID.IsEqual(req) && s.counts(a.Issuer)
}

// expelCarriesOut reports whether r is an ExpelRequest whose Subject the root
// key's Expulsion names; that Expulsion is its Approval.
func (s *swarmSnapshot) expelCarriesOut(r astral.Object, p requestParties) bool {
	if _, ok := r.(*user.SignedExpelRequest); !ok || p.subject.IsZero() {
		return false
	}
	for _, e := range s.expels {
		if !e.IsNil() && s.isRoot(e.Issuer) && e.Subject.IsEqual(p.subject) {
			return true
		}
	}
	return false
}

// approvalsTouching returns the counting Approvals of stored requests that
// node filed, carried in or that name it, including since-declined ones.
func (s *swarmSnapshot) approvalsTouching(node *astral.Identity) []*user.SignedApproval {
	if node.IsZero() {
		return nil
	}
	var list []*user.SignedApproval
	for _, a := range s.approvals {
		if a.IsNil() || !s.counts(a.Issuer) {
			continue
		}
		if _, p, ok := s.request(a.RequestID); ok && p.touches(node) {
			list = append(list, a)
		}
	}
	return list
}

// carriedOut returns the stored contracts whose ApprovalID is approval, in any
// expiry or expel state.
func (s *swarmSnapshot) carriedOut(approval *astral.ObjectID) []*auth.SignedContract {
	if approval == nil {
		return nil
	}
	var list []*auth.SignedContract
	for _, c := range s.contracts {
		if !c.IsNil() && c.ApprovalID != nil && c.ApprovalID.IsEqual(approval) {
			list = append(list, c)
		}
	}
	return list
}

// approvedNotCarriedOut reports whether req is approved by an Approval while
// no contract carries out any of its counting Approvals. An ExpelRequest is
// carried out by its Expulsion, which is no Approval, so it never reports true.
func (s *swarmSnapshot) approvedNotCarriedOut(req *astral.ObjectID) bool {
	if s.requestStatus(req) != user.RequestApproved {
		return false
	}
	approved := false
	for id, a := range s.approvals {
		if !s.countingApproval(a, req) {
			continue
		}
		if len(s.carriedOut(&id)) > 0 {
			return false
		}
		approved = true
	}
	return approved
}
