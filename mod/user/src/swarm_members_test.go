package user

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// memberQuery returns the staged membership contracts, or err, for any filter.
type memberQuery struct {
	found []*auth.SignedContract
	err   error
}

func (q *memberQuery) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder  { return q }
func (q *memberQuery) WithSubject(*astral.Identity) authmod.ContractQueryBuilder { return q }
func (q *memberQuery) WithAction(...astral.Object) authmod.ContractQueryBuilder  { return q }
func (q *memberQuery) Find(*astral.Context) ([]*auth.SignedContract, error)      { return q.found, q.err }

type memberAuth struct {
	authmod.Module
	query *memberQuery
}

func (a *memberAuth) SignedContracts() authmod.ContractQueryBuilder { return a.query }

// eventRecorder records every emitted event's data.
type eventRecorder struct {
	mu   sync.Mutex
	data []astral.Object
}

func (r *eventRecorder) Emit(data astral.Object) *events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, data)
	return &events.Event{Data: data}
}

// take returns the recorded events and forgets them.
func (r *eventRecorder) take() []astral.Object {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.data
	r.data = nil
	return d
}

func memberContract(t *testing.T, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()
	c, err := user.NewNodeContract(issuer, subject, false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &auth.SignedContract{Contract: c}
}

type memberEnv struct {
	mod    *Module
	query  *memberQuery
	events *eventRecorder
	issuer *astral.Identity
}

func newMemberEnv(t *testing.T) *memberEnv {
	issuer := astral.GenerateIdentity()
	mod := banModule(t, issuer)
	e := &memberEnv{mod: mod, query: &memberQuery{}, events: &eventRecorder{}, issuer: issuer}
	mod.Auth = &memberAuth{query: e.query}
	mod.Events = e.events
	return e
}

// One event per membership change, through the indexing hook and through an
// expulsion; none when the member set is unchanged.
func TestPublishSwarmEmitsOneEventPerChange(t *testing.T) {
	e := newMemberEnv(t)
	a := astral.GenerateIdentity()
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, a)}
	e.mod.recordSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("recording the first set emitted %v", got)
	}

	b := astral.GenerateIdentity()
	cb := memberContract(t, e.issuer, b)
	e.query.found = append(e.query.found, cb)
	e.mod.onContractIndexed(cb)
	got := e.events.take()
	if len(got) != 1 {
		t.Fatalf("%d events; want one joined", len(got))
	}
	if j, ok := got[0].(*user.SwarmMemberJoined); !ok || !j.NodeID.IsEqual(b) {
		t.Fatalf("got %v; want %v joined", got[0], b)
	}

	e.mod.onContractIndexed(cb)
	e.mod.publishSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("an unchanged set emitted %v", got)
	}

	if err := e.mod.db.StoreExpulsion(sampleSigned(e.issuer, b)); err != nil {
		t.Fatal(err)
	}
	e.mod.publishSwarm()
	got = e.events.take()
	if len(got) != 1 {
		t.Fatalf("%d events; want one left", len(got))
	}
	if l, ok := got[0].(*user.SwarmMemberLeft); !ok || !l.NodeID.IsEqual(b) {
		t.Fatalf("got %v; want %v left", got[0], b)
	}
}

// A contract that is not a node contract never republishes the set.
func TestIndexedNonNodeContractPublishesNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.mod.recordSwarm()
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, astral.GenerateIdentity())}
	e.mod.onContractIndexed(&auth.SignedContract{Contract: &auth.Contract{Issuer: e.issuer, Subject: astral.GenerateIdentity()}})
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("a non-node contract emitted %v", got)
	}
}

// Before Run records the first set, nothing is published.
func TestPublishSwarmBeforeRecordEmitsNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, astral.GenerateIdentity())}
	e.mod.publishSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("publishing before Run emitted %v", got)
	}
}
