package auth

import (
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Contract is the unsigned body of an authorization grant from Issuer to Subject.
// Wrap it in SignedContract before indexing or verifying.
type Contract struct {
	Issuer  *astral.Identity
	Subject *astral.Identity
	Permits []*Permit

	ExpiresAt astral.Time
}

type Permit struct {
	Action      astral.String8 // object type of an action
	Constraints *astral.Bundle // list of constraints
	Delegation  astral.Uint8   // hops allowed below a link carrying this permit; 0 = non-delegable
}

var _ astral.Object = &Permit{}
var _ crypto.SignableTextObject = &Contract{}

func (Permit) ObjectType() string { return "mod.auth.permit" }

func (p Permit) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&p).WriteTo(w) }
func (p *Permit) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(p).ReadFrom(r) }

func (p Permit) MarshalJSON() ([]byte, error)  { return astral.Objectify(&p).MarshalJSON() }
func (p *Permit) UnmarshalJSON(b []byte) error { return astral.Objectify(p).UnmarshalJSON(b) }

// Allows reports whether p permits the action: the action type must match and,
// for Constrainable actions, the permit's constraints must pass.
func (p *Permit) Allows(action ActionObject) bool {
	if string(p.Action) != action.ObjectType() {
		return false
	}
	if ca, ok := action.(Constrainable); ok {
		return ca.ApplyConstraints(p.Constraints)
	}
	return true
}

// HasPermit returns all permits in this contract that match the given action type.
// Empty result means the contract grants no such permission.
func (c *Contract) HasPermit(action string) []*Permit {
	if c.Permits == nil {
		return nil
	}
	var result []*Permit
	for _, p := range c.Permits {
		if string(p.Action) == action {
			result = append(result, p)
		}
	}
	return result
}

func (Contract) ObjectType() string { return "mod.auth.contract" }

func (c Contract) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&c).WriteTo(w) }
func (c *Contract) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(c).ReadFrom(r) }

func (c Contract) MarshalJSON() ([]byte, error)  { return astral.Objectify(&c).MarshalJSON() }
func (c *Contract) UnmarshalJSON(b []byte) error { return astral.Objectify(c).UnmarshalJSON(b) }

func (c *Contract) SignableHash() []byte {
	id, err := astral.ResolveObjectID(c)
	if err != nil {
		return nil
	}
	return id.Hash[:]
}

func (c *Contract) SignableText() string {
	return fmt.Sprintf(
		"%s grants %s permits (%d) until %s",
		c.Issuer.String(),
		c.Subject.String(),
		len(c.Permits),
		c.ExpiresAt.Time().Format("2006-01-02 15:04:05"),
	)
}

func init() {
	_ = astral.Add(&Contract{})
	_ = astral.Add(&Permit{})
}
