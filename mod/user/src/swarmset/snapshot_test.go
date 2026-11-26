package swarmset

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestParseRequestReadsEachKindOnce (criterion 10): one parse names a
// request's filer, carrier and subject, and refuses a nil body and any other
// object.
func TestParseRequestReadsEachKindOnce(t *testing.T) {
	filer := astral.GenerateIdentity()
	cr := contractRequest(filer)
	jr := joinRequest(filer, user.TierMember)
	er := expelRequest(filer)

	for _, c := range []struct {
		obj                     astral.Object
		filer, carrier, subject *astral.Identity
		expel                   bool
	}{
		{cr, filer, nil, cr.Identity, false},
		{jr, nil, filer, jr.Joiner, false},
		{er, filer, nil, er.Subject, true},
	} {
		r, ok := ParseRequest(c.obj)
		if !ok || !r.Filer.IsEqual(c.filer) || !r.Carrier.IsEqual(c.carrier) || !r.Subject.IsEqual(c.subject) ||
			r.isExpel() != c.expel || string(r.Kind) != c.obj.ObjectType() {
			t.Fatalf("ParseRequest(%v) = %+v, %v", c.obj.ObjectType(), r, ok)
		}
	}
	for _, obj := range []astral.Object{&user.SignedContractRequest{}, &user.SignedApproval{}, &astral.Ack{}} {
		if _, ok := ParseRequest(obj); ok {
			t.Fatalf("ParseRequest accepted %v", obj.ObjectType())
		}
	}
}

// TestIndexHoldsNoNilEntries (criterion 10): an entry with a nil body, and an
// answer that names no request, are left out of the index.
func TestIndexHoldsNoNilEntries(t *testing.T) {
	x := NewIndex()
	for _, obj := range []astral.Object{
		&user.SignedContractRequest{},
		&user.SignedApproval{},
		&user.SignedApproval{Approval: user.NewApproval(astral.GenerateIdentity(), nil), IssuerSig: sigBy(astral.GenerateIdentity())},
		&user.SignedDecline{},
		&auth.SignedContract{},
		&user.SignedExpulsion{},
	} {
		x.Add(&astral.ObjectID{Size: uint64(len(x.entries) + 1)}, obj)
	}
	if len(x.entries)+len(x.requests)+len(x.approvals)+len(x.declines)+len(x.contracts)+len(x.expels) != 0 {
		t.Fatalf("the index holds nil entries: %+v", x)
	}
}
