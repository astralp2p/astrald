package user

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func TestValidateActiveContract(t *testing.T) {
	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{Deps: Deps{Auth: &indexAuth{}}, node: &identityNode{id: nodeID}}

	cases := []struct {
		name     string
		contract func() *auth.SignedContract
		check    func(error) bool
		want     string
	}{
		{"valid membership", func() *auth.SignedContract {
			return nodeContract(userID, nodeID)
		}, func(err error) bool { return err == nil }, "nil"},

		{"another subject", func() *auth.SignedContract {
			return nodeContract(userID, astral.GenerateIdentity())
		}, func(err error) bool {
			return err != nil && err.Error() == "local node is not the subject of the contract"
		}, "local node is not the subject of the contract"},

		{"expired", func() *auth.SignedContract {
			sc := nodeContract(userID, nodeID)
			sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
			return sc
		}, func(err error) bool { return errors.Is(err, auth.ErrContractExpired) }, auth.ErrContractExpired.Error()},

		{"no membership permit", func() *auth.SignedContract {
			return contractWith(userID, nodeID, auth.SeeObjectsAction{}.ObjectType())
		}, func(err error) bool {
			return err != nil && err.Error() == "contract does not grant swarm membership"
		}, "contract does not grant swarm membership"},

		{"forged issuer signature", func() *auth.SignedContract {
			sc := nodeContract(userID, nodeID)
			sc.IssuerSig = forgedSig()
			return sc
		}, func(err error) bool {
			return err != nil && strings.HasPrefix(err.Error(), "verify:")
		}, "an error prefixed verify:"},
	}

	for _, c := range cases {
		if err := mod.validateActiveContract(c.contract()); !c.check(err) {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.want)
		}
	}
}
