package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// withdrawingAuth records the contract withdrawal an op asks of the auth module.
type withdrawingAuth struct {
	*recordingAuth
	count     int
	issuer    *astral.Identity
	action    string
	withdrawn int
}

func (a *withdrawingAuth) WithdrawContracts(issuer *astral.Identity, action astral.Object) (int, error) {
	a.issuer, a.action = issuer, action.ObjectType()
	a.withdrawn++
	return a.count, nil
}

var _ authmod.Module = &withdrawingAuth{}

// TestWithdrawRelayWithdrawsRelayContractOfIdentity: an authorized operator's
// call withdraws the RelayForAction contracts of the named identity and
// answers; with none to withdraw it answers an error.
func TestWithdrawRelayWithdrawsRelayContractOfIdentity(t *testing.T) {
	for _, c := range []struct {
		name  string
		count int
		fails bool
	}{{"contract held", 1, false}, {"no contract", 0, true}} {
		t.Run(c.name, func(t *testing.T) {
			auth := &withdrawingAuth{recordingAuth: &recordingAuth{verdict: true}, count: c.count}
			mod := &Module{Deps: Deps{Auth: auth, Dir: &namingDir{}}, log: log.New(nil)}
			app, caller := astral.GenerateIdentity(), astral.GenerateIdentity()
			w := newRecordingWriter()

			err := route(t, mod.OpWithdrawRelay, caller, "apphost.withdraw_relay?identity="+app.String(), w)
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if c.fails {
				if got, want := w.refusal(t), "relay contract not found"; got != want {
					t.Fatalf("answered %q; want %q", got, want)
				}
			} else {
				waitAdminManageAppsAnswer(t, w)
				if w.written() == 0 {
					t.Fatal("the op answered nothing")
				}
			}

			if auth.withdrawn != 1 || !auth.issuer.IsEqual(app) || auth.action != (nodes.RelayForAction{}).ObjectType() {
				t.Fatalf("withdrew %v time(s) for %v action %q; want once for %v relay_for", auth.withdrawn, auth.issuer, auth.action, app)
			}
		})
	}
}
