package swarmset

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Index is one read of the set, keyed by entry ID: every entry filed under its kind, each request parsed once.
// It holds no nil entries: an entry with a nil body, and an answer that names no request, are left out.
type Index struct {
	entries   map[astral.ObjectID]astral.Object
	requests  map[astral.ObjectID]Request
	approvals map[astral.ObjectID]*user.SignedApproval
	declines  []*user.SignedDecline
	contracts map[astral.ObjectID]*auth.SignedContract
	expels    map[astral.ObjectID]*user.SignedExpulsion
}

// Request is a stored request, read once (ParseRequest).
type Request struct {
	Obj  astral.Object
	Kind astral.String8 // the signed request's object type
	// Filer is ContractRequest.FiledBy or ExpelRequest.FiledBy.
	Filer *astral.Identity
	// Carrier is JoinRequest.Carrier.
	Carrier *astral.Identity
	// Subject is the identity the request names: ContractRequest.Identity, JoinRequest.Joiner or
	// ExpelRequest.Subject.
	Subject *astral.Identity
	FiledAt astral.Time
	Timeout astral.Time
}

// ParseRequest reads a signed request; ok is false for any other object and for a nil body.
func ParseRequest(obj astral.Object) (r Request, ok bool) {
	switch e := obj.(type) {
	case *user.SignedContractRequest:
		if e.IsNil() {
			return r, false
		}
		r = Request{Filer: e.FiledBy, Subject: e.Identity, FiledAt: e.FiledAt, Timeout: e.Timeout}
	case *user.SignedJoinRequest:
		if e.IsNil() {
			return r, false
		}
		r = Request{Carrier: e.Carrier, Subject: e.Joiner, FiledAt: e.FiledAt, Timeout: e.Timeout}
	case *user.SignedExpelRequest:
		if e.IsNil() {
			return r, false
		}
		r = Request{Filer: e.FiledBy, Subject: e.Subject, FiledAt: e.FiledAt, Timeout: e.Timeout}
	default:
		return r, false
	}
	r.Obj, r.Kind = obj, astral.String8(obj.ObjectType())
	return r, true
}

// isExpel reports whether r is an ExpelRequest.
func (r Request) isExpel() bool {
	_, ok := r.Obj.(*user.SignedExpelRequest)
	return ok
}

// NewIndex returns an empty index.
func NewIndex() *Index {
	return &Index{
		entries:   map[astral.ObjectID]astral.Object{},
		requests:  map[astral.ObjectID]Request{},
		approvals: map[astral.ObjectID]*user.SignedApproval{},
		contracts: map[astral.ObjectID]*auth.SignedContract{},
		expels:    map[astral.ObjectID]*user.SignedExpulsion{},
	}
}

// Add files obj under its kind. An object of no entry kind, a nil entry and an entry already held are ignored.
func (x *Index) Add(id *astral.ObjectID, obj astral.Object) {
	if id == nil {
		return
	}
	if _, held := x.entries[*id]; held || !x.file(*id, obj) {
		return
	}
	x.entries[*id] = obj
}

// file files obj in the map of its kind and reports whether it was one.
func (x *Index) file(id astral.ObjectID, obj astral.Object) bool {
	switch e := obj.(type) {
	case *user.SignedApproval:
		if e.IsNil() || e.RequestID == nil {
			return false
		}
		x.approvals[id] = e
	case *user.SignedDecline:
		if e.IsNil() || e.RequestID == nil {
			return false
		}
		x.declines = append(x.declines, e)
	case *auth.SignedContract:
		if e.IsNil() {
			return false
		}
		x.contracts[id] = e
	case *user.SignedExpulsion:
		if e.IsNil() {
			return false
		}
		x.expels[id] = e
	default:
		r, ok := ParseRequest(obj)
		if !ok {
			return false
		}
		x.requests[id] = r
	}
	return true
}

// Entry returns the entry with entry ID id.
func (x *Index) Entry(id *astral.ObjectID) (astral.Object, bool) {
	if id == nil {
		return nil, false
	}
	obj, ok := x.entries[*id]
	return obj, ok
}

// Snapshot reads an Index as of Now, with Root as the root key. Its methods are pure functions of these three.
//
// note: stored entries passed the entry check, so signatures and an answer's right to answer (user.MayAnswer)
// are trusted. What changes after the check is applied here: Timeouts, expiry, Declines and expels.
type Snapshot struct {
	*Index
	Root *astral.Identity
	Now  time.Time
}

// IsRoot reports whether id is the root key root. A zero root is nobody's.
func IsRoot(root, id *astral.Identity) bool {
	return !root.IsZero() && id.IsEqual(root)
}

// Expelled reports whether a stored Expulsion names id as its Subject.
func (s *Snapshot) Expelled(id *astral.Identity) bool {
	if id.IsZero() {
		return false
	}
	for _, e := range s.expels {
		if e.Subject.IsEqual(id) {
			return true
		}
	}
	return false
}

// counts reports whether an answer or contract signed by signer counts: an expel beats everything signed by the
// expelled identity.
func (s *Snapshot) counts(signer *astral.Identity) bool {
	return !signer.IsZero() && !s.Expelled(signer)
}
