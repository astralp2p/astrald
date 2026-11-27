package swarmset

import (
	"slices"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Request returns the stored request with entry ID req.
func (s *Snapshot) Request(req *astral.ObjectID) (Request, bool) {
	if req == nil {
		return Request{}, false
	}
	r, ok := s.requests[*req]
	return r, ok
}

// RequestIDs returns the entry IDs of every stored request, in no defined order.
func (s *Snapshot) RequestIDs() []*astral.ObjectID {
	ids := make([]*astral.ObjectID, 0, len(s.requests))
	for id := range s.requests {
		ids = append(ids, &id)
	}
	return ids
}

// RequestStatus returns the status of req: fulfilled > declined > approved > expired > pending. An unknown req
// is pending.
//
// todo: user.RequestOffered once offers answer ContractRequests, and approved for an ExpelRequest only.
func (s *Snapshot) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	r, ok := s.Request(req)
	switch {
	case !ok:
		return user.RequestPending
	case s.fulfilled(req, r):
		return user.RequestFulfilled
	case s.declined(req, r):
		return user.RequestDeclined
	case len(s.approvalsOf(req)) > 0 || s.expulsionOf(r) != nil:
		return user.RequestApproved
	case s.Now.After(r.Timeout.Time()):
		// why: the same boundary as the request bodies' Expired: at the Timeout itself the request is still
		// answerable.
		return user.RequestExpired
	}
	return user.RequestPending
}

// fulfilled reports whether a stored contract with both signatures names req as its RequestID and is given to
// the identity r names or to r's filer: a membership for a JoinRequest's Joiner, the host's contract to the app
// of a ContractRequest, or an approver's contract to its filer. An ExpelRequest is never fulfilled. Nothing that
// follows changes it.
//
// why the parties: a contract's issuer chooses its RequestID, so a contract to a bystander fulfils nothing.
func (s *Snapshot) fulfilled(req *astral.ObjectID, r Request) bool {
	if r.isExpel() {
		return false
	}
	for _, c := range s.ContractsOf(req) {
		if c.IssuerSig != nil && c.SubjectSig != nil &&
			(c.Subject.IsEqual(r.Subject) || (!r.Filer.IsZero() && c.Subject.IsEqual(r.Filer))) {
			return true
		}
	}
	return false
}

// declined reports whether a counting Decline names req, or the request was filed by, carried by or names an
// expelled identity. A Decline closes the request; it takes back no contract (fulfilled comes first).
//
// why not an ExpelRequest's Subject: expelling it carries the request out.
func (s *Snapshot) declined(req *astral.ObjectID, r Request) bool {
	if s.Expelled(r.Filer) || s.Expelled(r.Carrier) || (!r.isExpel() && s.Expelled(r.Subject)) {
		return true
	}
	for _, d := range s.declines {
		if d.RequestID.IsEqual(req) && s.counts(d.Issuer) {
			return true
		}
	}
	return false
}

// approvalsOf returns the entry IDs of the counting Approvals of req, whenever they were given, lowest first.
func (s *Snapshot) approvalsOf(req *astral.ObjectID) []*astral.ObjectID {
	var ids []*astral.ObjectID
	for id, a := range s.approvals {
		if a.RequestID.IsEqual(req) && s.counts(a.Issuer) {
			ids = append(ids, &id)
		}
	}
	slices.SortFunc(ids, func(a, b *astral.ObjectID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

// expulsionOf returns the entry ID of the root key's Expulsion naming the Subject of r when r is an ExpelRequest;
// that Expulsion is its Approval. It returns nil otherwise.
func (s *Snapshot) expulsionOf(r Request) *astral.ObjectID {
	if !r.isExpel() || r.Subject.IsZero() {
		return nil
	}
	var found *astral.ObjectID
	for id, e := range s.expels {
		if IsRoot(s.Root, e.Issuer) && e.Subject.IsEqual(r.Subject) && (found == nil || id.String() < found.String()) {
			found = &id
		}
	}
	return found
}

// ApprovalOf returns the entry ID that approves req: its lowest counting Approval, or the Expulsion that carries
// out an ExpelRequest. It returns nil when nothing approves req.
func (s *Snapshot) ApprovalOf(req *astral.ObjectID) *astral.ObjectID {
	if ids := s.approvalsOf(req); len(ids) > 0 {
		return ids[0]
	}
	r, ok := s.Request(req)
	if !ok {
		return nil
	}
	return s.expulsionOf(r)
}

// ApprovalsTouching returns the counting Approvals of stored requests that node filed, carried in or that name
// it, including since-declined ones.
func (s *Snapshot) ApprovalsTouching(node *astral.Identity) []*user.SignedApproval {
	if node.IsZero() {
		return nil
	}
	var list []*user.SignedApproval
	for _, a := range s.approvals {
		if !s.counts(a.Issuer) {
			continue
		}
		if r, ok := s.Request(a.RequestID); ok && r.touches(node) {
			list = append(list, a)
		}
	}
	return list
}

// ContractsOf returns the stored contracts whose RequestID is req, in any signature, expiry or expel state.
func (s *Snapshot) ContractsOf(req *astral.ObjectID) []*auth.SignedContract {
	if req == nil {
		return nil
	}
	var list []*auth.SignedContract
	for _, c := range s.contracts {
		if c.RequestID != nil && c.RequestID.IsEqual(req) {
			list = append(list, c)
		}
	}
	return list
}

// ApprovedNotCarriedOut reports whether req is approved by an Approval while no contract fulfils it. An
// ExpelRequest is carried out by its Expulsion, which is no Approval, so it never reports true.
func (s *Snapshot) ApprovedNotCarriedOut(req *astral.ObjectID) bool {
	return s.RequestStatus(req) == user.RequestApproved && len(s.approvalsOf(req)) > 0
}
