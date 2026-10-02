package user

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	alog "github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
)

// appContractQuery records the filters the sync asks for and returns the staged contracts.
type appContractQuery struct {
	subject *astral.Identity
	actions []astral.Object
	found   []*auth.SignedContract
}

func (q *appContractQuery) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder {
	panic("the app contract sync does not filter by issuer")
}

func (q *appContractQuery) WithSubject(id *astral.Identity) authmod.ContractQueryBuilder {
	q.subject = id
	return q
}

func (q *appContractQuery) WithAction(a ...astral.Object) authmod.ContractQueryBuilder {
	q.actions = append(q.actions, a...)
	return q
}

func (q *appContractQuery) Find(*astral.Context) ([]*auth.SignedContract, error) {
	return q.found, nil
}

type appContractAuth struct {
	authmod.Module
	query *appContractQuery
}

func (a *appContractAuth) SignedContracts() authmod.ContractQueryBuilder { return a.query }

type pushRecorder struct {
	objects.Module
	mu     sync.Mutex
	target []*astral.Identity
	pushed []astral.Object
}

func (p *pushRecorder) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = append(p.target, target)
	p.pushed = append(p.pushed, obj)
	return nil
}

// TestSyncAppContractsPushesTheRelayContractsOfLocalApps: the sibling receives
// every relay contract naming this node as host, so it can route to those apps.
func TestSyncAppContractsPushesTheRelayContractsOfLocalApps(t *testing.T) {
	nodeID, sibling := astral.GenerateIdentity(), astral.GenerateIdentity()
	app1 := &auth.SignedContract{Contract: &auth.Contract{Issuer: astral.GenerateIdentity(), Subject: nodeID}}
	app2 := &auth.SignedContract{Contract: &auth.Contract{Issuer: astral.GenerateIdentity(), Subject: nodeID}}
	query := &appContractQuery{found: []*auth.SignedContract{app1, app2}}
	pushes := &pushRecorder{}

	mod := &Module{node: &identityNode{id: nodeID}, log: alog.New(nodeID)}
	mod.Deps.Auth = &appContractAuth{query: query}
	mod.Deps.Objects = pushes

	mod.syncAppContracts(astral.NewContext(nil), sibling)

	if !query.subject.IsEqual(nodeID) {
		t.Fatalf("asked for contracts of subject %v; want this node %v", query.subject, nodeID)
	}
	if len(query.actions) != 1 || query.actions[0].ObjectType() != (nodes.RelayForAction{}).ObjectType() {
		t.Fatalf("asked for actions %v; want only the relay-for action", query.actions)
	}
	if len(pushes.pushed) != 2 || pushes.pushed[0] != app1 || pushes.pushed[1] != app2 {
		t.Fatalf("pushed %v; want both app contracts", pushes.pushed)
	}
	for _, target := range pushes.target {
		if !target.IsEqual(sibling) {
			t.Fatalf("pushed to %v; want the sibling %v", target, sibling)
		}
	}
}
