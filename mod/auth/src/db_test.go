package auth

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func signedContract(issuer, subject *astral.Identity, validFor time.Duration, permits ...*auth.Permit) *auth.SignedContract {
	return &auth.SignedContract{
		Contract: &auth.Contract{
			Issuer:    issuer,
			Subject:   subject,
			Permits:   permits,
			ExpiresAt: astral.Time(time.Now().Add(validFor)),
		},
		IssuerSig:  dummySig(),
		SubjectSig: dummySig(),
	}
}

func storeContract(t *testing.T, mod *Module, sc *auth.SignedContract) *astral.ObjectID {
	t.Helper()

	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}

	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("resolve object id: %v", err)
	}
	return id
}

func TestContractIndexExistenceAndHold(t *testing.T) {
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()

	cases := []struct {
		name       string
		validFor   time.Duration
		store      bool
		wantExists bool
		wantHeld   bool
	}{
		{"unknown object", time.Hour, false, false, false},
		{"active contract", time.Hour, true, true, true},
		{"expired contract", -time.Hour, true, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := testModule(t)
			sc := signedContract(issuer, subject, c.validFor, permit(0))

			id, err := astral.ResolveObjectID(sc)
			if err != nil {
				t.Fatalf("resolve object id: %v", err)
			}
			if c.store {
				storeContract(t, mod, sc)
			}

			if got := mod.db.contractExists(id); got != c.wantExists {
				t.Errorf("contractExists = %v, want %v", got, c.wantExists)
			}
			if got := mod.HoldObject(id); got != c.wantHeld {
				t.Errorf("HoldObject = %v, want %v", got, c.wantHeld)
			}
		})
	}
}

func TestStoreSignedContractTwiceKeepsOnePermitSet(t *testing.T) {
	mod := testModule(t)
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0), permit(1))

	storeContract(t, mod, sc)
	id := storeContract(t, mod, sc)

	rows, err := mod.db.findContractPermits(id)
	if err != nil {
		t.Fatalf("find permits: %v", err)
	}
	if len(rows) != len(sc.Permits) {
		t.Fatalf("permit rows = %d, want %d", len(rows), len(sc.Permits))
	}
}

func TestContractQueryFilters(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	a, b, s := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	storeContract(t, mod, signedContract(a, s, time.Hour, permit(0), permit(1)))
	storeContract(t, mod, signedContract(b, s, time.Hour, permit(0)))

	bySubject, err := mod.SignedContracts().WithSubject(s).WithAction(&testAction{}).Find(ctx)
	if err != nil {
		t.Fatalf("find by subject and action: %v", err)
	}
	if len(bySubject) != 2 {
		t.Fatalf("find by subject and action returned %d contracts, want 2", len(bySubject))
	}
	issuers := map[string]int{}
	for _, sc := range bySubject {
		issuers[sc.Issuer.String()]++
	}
	if issuers[a.String()] != 1 || issuers[b.String()] != 1 {
		t.Fatalf("find by subject and action: contracts per issuer = %v, want one from %v and one from %v", issuers, a, b)
	}

	byIssuer, err := mod.SignedContracts().WithIssuer(a).Find(ctx)
	if err != nil {
		t.Fatalf("find by issuer: %v", err)
	}
	if len(byIssuer) != 1 {
		t.Fatalf("find by issuer returned %d contracts, want 1", len(byIssuer))
	}
	if !byIssuer[0].Issuer.IsEqual(a) {
		t.Fatalf("find by issuer returned a contract from %v, want %v", byIssuer[0].Issuer, a)
	}
	if n := len(byIssuer[0].Permits); n != 2 {
		t.Fatalf("find by issuer returned %d permits, want 2", n)
	}
}

func TestHoldObjectFailsClosedOnDBError(t *testing.T) {
	mod := testModule(t)
	// why: the contract is expired, so only the error path answers true.
	id := storeContract(t, mod, signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), -time.Hour, permit(0)))

	sqlDB, err := mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if !mod.HoldObject(id) {
		t.Fatal("HoldObject = false after a DB error, want true")
	}
}
