package auth

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

// SignIssuer signs the contract as the issuer; returns auth.ErrAlreadySigned if IssuerSig is already set.
func (mod *Module) SignIssuer(ctx *astral.Context, signed *auth.SignedContract) (*crypto.Signature, error) {
	if signed.IssuerSig != nil {
		return nil, auth.ErrAlreadySigned
	}
	sig, err := mod.signAs(ctx, secp256k1.FromIdentity(signed.Issuer), signed.Contract)
	if err != nil {
		return nil, fmt.Errorf("sign as issuer: %w", err)
	}
	signed.IssuerSig = sig
	return sig, nil
}

// SignSubject signs the contract as the subject; returns auth.ErrAlreadySigned if SubjectSig is already set.
func (mod *Module) SignSubject(ctx *astral.Context, signed *auth.SignedContract) (*crypto.Signature, error) {
	if signed.SubjectSig != nil {
		return nil, auth.ErrAlreadySigned
	}
	sig, err := mod.signAs(ctx, secp256k1.FromIdentity(signed.Subject), signed.Contract)
	if err != nil {
		return nil, fmt.Errorf("sign as subject: %w", err)
	}
	signed.SubjectSig = sig
	return sig, nil
}

func (mod *Module) SignContract(ctx *astral.Context, signed *auth.SignedContract) error {
	_, err := mod.SignIssuer(ctx, signed)
	if err != nil {
		return err
	}

	_, err = mod.SignSubject(ctx, signed)
	return err
}

func (mod *Module) signAs(ctx *astral.Context, key *crypto.PublicKey, c *auth.Contract) (*crypto.Signature, error) {
	return mod.Crypto.Sign(ctx, key, c)
}

// VerifyIssuer checks that IssuerSig is present and cryptographically valid for the contract.
func (mod *Module) VerifyIssuer(sc *auth.SignedContract) error {
	if sc.IssuerSig == nil {
		return errors.New("issuer signature is missing")
	}
	if err := mod.verifySig(secp256k1.FromIdentity(sc.Issuer), sc.IssuerSig, sc.Contract); err != nil {
		return fmt.Errorf("issuer sig: %w", err)
	}
	return nil
}

// VerifySubject checks that SubjectSig is present and cryptographically valid for the contract.
func (mod *Module) VerifySubject(sc *auth.SignedContract) error {
	if sc.SubjectSig == nil {
		return errors.New("subject signature is missing")
	}
	if err := mod.verifySig(secp256k1.FromIdentity(sc.Subject), sc.SubjectSig, sc.Contract); err != nil {
		return fmt.Errorf("subject sig: %w", err)
	}
	return nil
}

func (mod *Module) VerifyContract(sc *auth.SignedContract) error {
	if err := mod.VerifyIssuer(sc); err != nil {
		return err
	}
	return mod.VerifySubject(sc)
}

func (mod *Module) verifySig(key *crypto.PublicKey, sig *crypto.Signature, contract *auth.Contract) error {
	return mod.Crypto.Verify(key, sig, contract)
}
