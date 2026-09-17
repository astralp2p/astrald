package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// note: every module here has a nil Crypto, so a guard that falls through to signing or verification panics.

func TestSignRefusesAnAlreadySignedContract(t *testing.T) {
	ctx := astral.NewContext(nil)
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()

	t.Run("SignIssuer", func(t *testing.T) {
		mod := &Module{}
		sc := signedContract(issuer, subject, time.Hour)
		sc.SubjectSig = nil
		sig := sc.IssuerSig

		if _, err := mod.SignIssuer(ctx, sc); !errors.Is(err, auth.ErrAlreadySigned) {
			t.Fatalf("SignIssuer err = %v, want %v", err, auth.ErrAlreadySigned)
		}
		if sc.IssuerSig != sig {
			t.Fatal("SignIssuer replaced the existing issuer signature")
		}
	})

	t.Run("SignSubject", func(t *testing.T) {
		mod := &Module{}
		sc := signedContract(issuer, subject, time.Hour)
		sc.IssuerSig = nil
		sig := sc.SubjectSig

		if _, err := mod.SignSubject(ctx, sc); !errors.Is(err, auth.ErrAlreadySigned) {
			t.Fatalf("SignSubject err = %v, want %v", err, auth.ErrAlreadySigned)
		}
		if sc.SubjectSig != sig {
			t.Fatal("SignSubject replaced the existing subject signature")
		}
	})

	t.Run("SignContract", func(t *testing.T) {
		mod := &Module{}
		sc := signedContract(issuer, subject, time.Hour)
		sc.SubjectSig = nil

		if err := mod.SignContract(ctx, sc); !errors.Is(err, auth.ErrAlreadySigned) {
			t.Fatalf("SignContract err = %v, want %v", err, auth.ErrAlreadySigned)
		}
		if sc.SubjectSig != nil {
			t.Fatal("SignContract set the subject signature after the issuer guard refused")
		}
	})
}

func TestVerifyReportsAMissingSignature(t *testing.T) {
	mod := &Module{}
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()
	unsigned := func() *auth.SignedContract {
		sc := signedContract(issuer, subject, time.Hour)
		sc.IssuerSig, sc.SubjectSig = nil, nil
		return sc
	}

	cases := []struct {
		name   string
		verify func(*auth.SignedContract) error
		want   string
	}{
		{"VerifyIssuer", mod.VerifyIssuer, "issuer signature is missing"},
		{"VerifySubject", mod.VerifySubject, "subject signature is missing"},
		{"VerifyContract", mod.VerifyContract, "issuer signature is missing"},
	}

	for _, c := range cases {
		err := c.verify(unsigned())
		if err == nil || err.Error() != c.want {
			t.Errorf("%s err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestIndexContractSkipsAnIndexedContract(t *testing.T) {
	mod := testModule(t)
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	storeContract(t, mod, sc)

	if err := mod.IndexContract(astral.NewContext(nil), sc); err != nil {
		t.Fatalf("IndexContract on an indexed contract: %v, want nil", err)
	}
}

func TestIndexContractRefusesAnUnsignedContract(t *testing.T) {
	mod := testModule(t)
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	sc.IssuerSig, sc.SubjectSig = nil, nil

	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("resolve object id: %v", err)
	}

	err = mod.IndexContract(astral.NewContext(nil), sc)
	if err == nil || !strings.Contains(err.Error(), "issuer signature is missing") {
		t.Fatalf("IndexContract err = %v, want one naming the missing issuer signature", err)
	}
	if mod.db.contractExists(id) {
		t.Fatal("an unsigned contract was indexed")
	}
}
