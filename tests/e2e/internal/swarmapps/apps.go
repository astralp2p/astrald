package main

import (
	"errors"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// Service is the wallet stand-in's service name.
const Service = "wallet"

// discoveryPermit is a ServiceDiscovery permit for Service on any node.
func discoveryPermit() (*auth.Permit, error) {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{Service}}}}
	if err := cs.Append(scope); err != nil {
		return nil, err
	}
	return &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}, nil
}

// askedPermits are the permits an app asks beyond the hosting ones the node
// adds to every registration.
func askedPermits(discovery bool) ([]*auth.Permit, error) {
	if !discovery {
		return nil, nil
	}
	p, err := discoveryPermit()
	if err != nil {
		return nil, err
	}
	return []*auth.Permit{p}, nil
}

// register mints an app on the node c reaches. The node files one
// ContractRequest for the hosting permits and the asked ones.
func register(ctx *astral.Context, c *astrald.Client, discovery bool) (map[string]any, error) {
	permits, err := askedPermits(discovery)
	if err != nil {
		return nil, err
	}
	tok, err := apphostcli.New(nil, c).RegisterWith(ctx, &apphostapi.RegisterRequest{Name: "swarm-state", ContractPermits: permits})
	if err != nil {
		return nil, err
	}
	return map[string]any{"identity": tok.Identity.String(), "token": string(tok.Token)}, nil
}

// request runs apphost.request_contract as the app c's token names: it files
// (or finds) the request for the hosting permits and the asked ones, and
// waits up to wait for its outcome.
func request(ctx *astral.Context, c *astrald.Client, wait time.Duration, discovery bool) (map[string]any, error) {
	asked, err := askedPermits(discovery)
	if err != nil {
		return nil, err
	}
	permits := append([]*auth.Permit{
		{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())},
		{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())},
	}, asked...)

	wctx, cancel := ctx.WithTimeout(wait)
	defer cancel()
	ch, err := c.QueryChannel(wctx, "apphost.request_contract", nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	for _, p := range permits {
		if err = ch.Send(p); err != nil {
			return nil, err
		}
	}
	if err = ch.Send(&astral.EOS{}); err != nil {
		return nil, err
	}

	out := map[string]any{}
	var contract *auth.SignedContract
	err = ch.Switch(
		func(id *astral.ObjectID) error { out["request_id"] = id.String(); return nil },
		func(sc *auth.SignedContract) error { contract = sc; return channel.ErrBreak },
		channel.PassErrors,
		channel.WithContext(wctx),
		channel.FailOnEOF(),
	)
	if contract != nil {
		out["contract"] = contractFacts(contract)
		return out, nil
	}
	if err == nil {
		err = errors.New("no contract")
	}
	out["error"] = err.Error()
	return out, nil
}

func contractFacts(sc *auth.SignedContract) map[string]any {
	f := map[string]any{"issuer": sc.Issuer.String(), "subject": sc.Subject.String()}
	if sc.ApprovalID != nil {
		f["approval_id"] = sc.ApprovalID.String()
	}
	return f
}

// createToken mints a token on the node c reaches for identity: an app
// installed on that node under an identity it already has.
func createToken(ctx *astral.Context, c *astrald.Client, identity string) (map[string]any, error) {
	id, err := astral.ParseIdentity(identity)
	if err != nil {
		return nil, err
	}
	tok, err := apphostcli.New(nil, c).CreateToken(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	return map[string]any{"token": string(tok.Token)}, nil
}
