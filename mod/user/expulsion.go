package user

import (
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Expulsion is the unsigned body of a swarm ban: Issuer permanently bans Subject
// from the swarm. Wrap it in SignedExpulsion before storing, propagating, or
// verifying. A ban is identity-level and irreversible.
type Expulsion struct {
	Issuer     *astral.Identity
	Subject    *astral.Identity
	ExpelledAt astral.Time
}

var _ crypto.SignableTextObject = &Expulsion{}

func (Expulsion) ObjectType() string { return "mod.user.expulsion" }

func (e Expulsion) WriteTo(w io.Writer) (int64, error)     { return astral.Objectify(&e).WriteTo(w) }
func (e *Expulsion) ReadFrom(src io.Reader) (int64, error) { return astral.Objectify(e).ReadFrom(src) }

func (e Expulsion) MarshalJSON() ([]byte, error)  { return astral.Objectify(&e).MarshalJSON() }
func (e *Expulsion) UnmarshalJSON(b []byte) error { return astral.Objectify(e).UnmarshalJSON(b) }

func (e *Expulsion) SignableHash() []byte {
	id, err := astral.ResolveObjectID(e)
	if err != nil {
		return nil
	}
	return id.Hash[:]
}

func (e *Expulsion) SignableText() string {
	return fmt.Sprintf("%s expels %s from the swarm", e.Issuer.String(), e.Subject.String())
}

// SignedExpulsion pairs an Expulsion body with the issuer's signature. It is the
// wire, stored, and propagated form of a ban.
type SignedExpulsion struct {
	*Expulsion
	IssuerSig *crypto.Signature
}

var _ astral.Object = &SignedExpulsion{}

func (SignedExpulsion) ObjectType() string { return "mod.user.signed_expulsion" }

func (e SignedExpulsion) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&e).WriteTo(w)
}
func (e *SignedExpulsion) ReadFrom(src io.Reader) (int64, error) {
	return astral.Objectify(e).ReadFrom(src)
}

func (e SignedExpulsion) MarshalJSON() ([]byte, error)  { return astral.Objectify(&e).MarshalJSON() }
func (e *SignedExpulsion) UnmarshalJSON(b []byte) error { return astral.Objectify(e).UnmarshalJSON(b) }

// IsNil guards against both a nil receiver and an embedded nil *Expulsion.
func (e *SignedExpulsion) IsNil() bool { return e == nil || e.Expulsion == nil }

func init() {
	_ = astral.Add(&Expulsion{})
	_ = astral.Add(&SignedExpulsion{})
}
