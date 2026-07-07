package auth

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

// SignedContract pairs a Contract body with the issuer and subject signatures.
// Either signature field may be nil before the signing step is complete.
type SignedContract struct {
	*Contract
	IssuerSig  *crypto.Signature
	SubjectSig *crypto.Signature
}

var _ astral.Object = &SignedContract{}

func (SignedContract) ObjectType() string { return "mod.auth.signed_contract" }

func (c SignedContract) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&c).WriteTo(w) }
func (c *SignedContract) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(c).ReadFrom(r) }

func (c SignedContract) MarshalJSON() ([]byte, error)  { return astral.Objectify(&c).MarshalJSON() }
func (c *SignedContract) UnmarshalJSON(b []byte) error { return astral.Objectify(c).UnmarshalJSON(b) }

// IsNil guards against both a nil receiver and an embedded nil *Contract.
func (c *SignedContract) IsNil() bool { return c == nil || c.Contract == nil }

func init() { _ = astral.Add(&SignedContract{}) }
