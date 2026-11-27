package user

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// useSwarm gives mod a SwarmView over an in-memory SwarmState holding, for
// each of members, a Member-tier membership userID signed, with userID as the
// root key. It returns the state, so a test adds entries later.
//
// note: the view trusts stored entries, so the signatures are placeholders.
func useSwarm(t testing.TB, mod *Module, userID *astral.Identity, members ...*astral.Identity) *storeState {
	t.Helper()
	state := newStoreState(t)
	for _, m := range members {
		addMembership(t, state, userID, m)
	}
	mod.swarmView.Store(newSwarmView(state, func() *astral.Identity { return userID }, time.Now))
	return state
}

// addMembership stores subject's Member-tier membership, signed by issuer.
func addMembership(t testing.TB, state *storeState, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()
	c, err := user.NewMembership(issuer, subject, user.TierMember, nil, time.Hour)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	sc := &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()}
	if err = state.merge(nil, sc); err != nil {
		t.Fatalf("store membership: %v", err)
	}
	return sc
}

// contractState is a SwarmState whose contracts are the ones contracts
// answers, so a test that stages members in a fake auth index sees them as
// SwarmState members. Its head moves on every read, so the view reads the
// staged set each time.
type contractState struct {
	usermod.SwarmState
	contracts func() ([]*auth.SignedContract, error)
	head      atomic.Uint64
}

func (s *contractState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	found, _ := s.contracts()
	for _, c := range found {
		if cid, err := astral.ResolveObjectID(c); err == nil && cid.IsEqual(id) {
			return c, nil
		}
	}
	return nil, errors.New("no such entry")
}

func (s *contractState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if kind != user.EntryContracts {
		return nil, nil
	}
	found, err := s.contracts()
	if err != nil {
		return nil, err
	}
	var ids []*astral.ObjectID
	for _, c := range found {
		if id, err := astral.ResolveObjectID(c); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *contractState) SwarmHead() uint64 { return s.head.Add(1) }

// useQuerySwarm gives mod a SwarmView over q's staged contracts, with userID
// as the root key.
func useQuerySwarm(mod *Module, userID *astral.Identity, q *memberQuery) {
	useContractSwarm(mod, userID, func() ([]*auth.SignedContract, error) {
		q.mu.Lock()
		defer q.mu.Unlock()
		return append([]*auth.SignedContract(nil), q.found...), q.err
	})
}

// useIndexSwarm gives mod a SwarmView over the contracts a seeds, with userID
// as the root key.
func useIndexSwarm(mod *Module, userID *astral.Identity, a *indexAuth) {
	useContractSwarm(mod, userID, func() ([]*auth.SignedContract, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		return append([]*auth.SignedContract(nil), a.index...), nil
	})
}

func useContractSwarm(mod *Module, userID *astral.Identity, contracts func() ([]*auth.SignedContract, error)) {
	mod.swarmView.Store(newSwarmView(&contractState{contracts: contracts}, func() *astral.Identity { return userID }, time.Now))
}

// pullNoSwarm is a nodes module with no behaviour: a SwarmState pull on a module whose store is closed does
// nothing, and the tests that use it read the swarm from a SwarmView fixture.
type pullNoSwarm struct{ nodesmod.Module }
