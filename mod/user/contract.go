package user

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// IsNodeContract reports whether a contract grants swarm membership.
func IsNodeContract(c *auth.Contract) bool {
	return len(c.HasPermit(SwarmMembershipAction{}.ObjectType())) > 0
}

// NewNodeContract creates a node contract granting swarm membership from issuer to subject.
// A management node also receives swarm-management permits (expel/adopt/info),
// delegable one hop so the node can contract them out to apps it hosts.
func NewNodeContract(issuer, subject *astral.Identity, managementNode bool, duration time.Duration) (*auth.Contract, error) {
	permits := []*auth.Permit{
		{Action: astral.String8(SwarmMembershipAction{}.ObjectType())},
	}
	if managementNode {
		permits = append(permits,
			&auth.Permit{Action: astral.String8(ExpelAction{}.ObjectType()), Delegation: 1},
			&auth.Permit{Action: astral.String8(AdoptAction{}.ObjectType()), Delegation: 1},
			&auth.Permit{Action: astral.String8(InfoAction{}.ObjectType()), Delegation: 1},
		)
	}

	return &auth.Contract{
		Issuer:    issuer,
		Subject:   subject,
		Permits:   permits,
		ExpiresAt: astral.Time(time.Now().Add(duration)),
	}, nil
}
