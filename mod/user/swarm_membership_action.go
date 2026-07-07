package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// SwarmMembershipAction is the permit type embedded in node contracts; its
// ObjectType string is what IsNodeContract checks to identify a valid swarm
// membership contract.
type SwarmMembershipAction struct {
	auth.Action
}

func (SwarmMembershipAction) ObjectType() string { return "mod.user.swarm_membership_action" }

func (a SwarmMembershipAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *SwarmMembershipAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() { _ = astral.Add(&SwarmMembershipAction{}) }
