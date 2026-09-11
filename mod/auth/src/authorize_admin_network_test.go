package auth

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// adminNetworkLink is one issuer→subject contract in an AdminNetwork chain. A
// negative validFor stores a contract that has already expired.
type adminNetworkLink struct {
	issuer, subject *astral.Identity
	permit          *auth.Permit
	validFor        time.Duration
}

// adminNetworkChain is a module whose direct rule allows only nodeID, standing in
// for the user module's AuthorizeAdminNetwork.
func adminNetworkChain(t *testing.T, nodeID *astral.Identity) *Module {
	t.Helper()

	mod := testModule(t)
	mod.Add(authmod.Func[*auth.AdminNetworkAction](func(_ *astral.Context, a *auth.AdminNetworkAction) bool {
		return a.Actor().IsEqual(nodeID)
	}))
	return mod
}

func storeAdminNetworkLink(t *testing.T, mod *Module, l adminNetworkLink) {
	t.Helper()

	sc := &auth.SignedContract{
		Contract: &auth.Contract{
			Issuer:    l.issuer,
			Subject:   l.subject,
			Permits:   []*auth.Permit{l.permit},
			ExpiresAt: astral.Time(time.Now().Add(l.validFor)),
		},
		IssuerSig:  dummySig(),
		SubjectSig: dummySig(),
	}

	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}
}

func adminNetworkPermit(delegation uint8, constrained bool) *auth.Permit {
	p := &auth.Permit{Action: astral.String8(auth.AdminNetworkAction{}.ObjectType()), Delegation: astral.Uint8(delegation)}
	if constrained {
		p.Constraints = astral.NewBundle()
		if err := p.Constraints.Append(&astral.Ack{}); err != nil {
			panic(err)
		}
	}
	return p
}

// TestAdminNetworkContractFromTheNodeAuthorizesTheApp is the signed-contract
// path: a node authorized here issues an app a permit, and the app is authorized
// as itself.
func TestAdminNetworkContractFromTheNodeAuthorizesTheApp(t *testing.T) {
	nodeID, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := adminNetworkChain(t, nodeID)
	storeAdminNetworkLink(t, mod, adminNetworkLink{nodeID, app, adminNetworkPermit(0, false), time.Hour})

	action := &auth.AdminNetworkAction{Action: auth.NewAction(app)}
	if !mod.Authorize(astral.NewContext(nil), action) {
		t.Fatal("the app holding a contract from the node was refused")
	}

	if !action.Actor().IsEqual(app) {
		t.Fatalf("the actor became %v; want the app %v", action.Actor(), app)
	}
}

// TestAdminNetworkContractChains covers the chains that must not authorize —
// absent, expired, constrained, from an unauthorized issuer, or re-delegated
// past the root link's allowance — beside the re-delegation that must.
func TestAdminNetworkContractChains(t *testing.T) {
	nodeID, mid, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	cases := []struct {
		name  string
		links []adminNetworkLink
		want  bool
	}{
		{"no contract", nil, false},
		{"an expired contract", []adminNetworkLink{{nodeID, app, adminNetworkPermit(0, false), -time.Hour}}, false},
		{"a constrained permit", []adminNetworkLink{{nodeID, app, adminNetworkPermit(0, true), time.Hour}}, false},
		{"an issuer the node does not authorize", []adminNetworkLink{{mid, app, adminNetworkPermit(0, false), time.Hour}}, false},
		{"a re-delegation the root link forbids", []adminNetworkLink{
			{nodeID, mid, adminNetworkPermit(0, false), time.Hour},
			{mid, app, adminNetworkPermit(0, false), time.Hour},
		}, false},
		{"a re-delegation the root link allows", []adminNetworkLink{
			{nodeID, mid, adminNetworkPermit(1, false), time.Hour},
			{mid, app, adminNetworkPermit(0, false), time.Hour},
		}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := adminNetworkChain(t, nodeID)
			for _, l := range c.links {
				storeAdminNetworkLink(t, mod, l)
			}

			got := mod.Authorize(astral.NewContext(nil), &auth.AdminNetworkAction{Action: auth.NewAction(app)})
			if got != c.want {
				t.Fatalf("authorized %v; want %v", got, c.want)
			}
		})
	}
}
