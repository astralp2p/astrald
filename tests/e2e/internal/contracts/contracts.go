// Package contracts waits, as an app, for the contract its host signs once
// the app's ContractRequest is approved. While a driver runs, the harness
// answerer (tests/lib/answerer.py) approves every ContractRequest.
package contracts

import (
	"errors"
	"fmt"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// Register registers a fresh app through registrar on the node at
// endpoint, asking permits, and waits until its host signed the contract its
// registration requested (Await).
func Register(ctx *astral.Context, registrar *astrald.Client, endpoint string, permits ...*auth.Permit) (*apphostapi.AccessToken, error) {
	tok, err := apphostcli.New(nil, registrar).Register(ctx, permits...)
	if err != nil {
		return nil, fmt.Errorf("apphost.register: %w", err)
	}
	if _, err = Await(ctx, astrald.New(apphost.NewRouter(endpoint, string(tok.Token))), permits...); err != nil {
		return nil, fmt.Errorf("the contract of %v: %w", tok.Identity, err)
	}
	return tok, nil
}

// Wait bounds Await: an answer, then the host's poll.
const Wait = 60 * time.Second

// Await runs apphost.request_contract as the app c's token names, for
// the hosting permits and asked, and returns the contract its host signs once
// the request is approved; the harness answerer (tests/lib/answerer.py)
// approves it while a driver runs.
//
// why the hosting permits first: apphost.register files them before the
// asked ones, and the node finds the request it filed only by the same
// permits, so a registered app waits on its registration's own request.
func Await(ctx *astral.Context, c *astrald.Client, asked ...*auth.Permit) (*auth.SignedContract, error) {
	permits := append([]*auth.Permit{
		{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())},
		{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())},
	}, asked...)
	return Request(ctx, c, permits...)
}

// RequestContract runs apphost.request_contract as the app c's token names
// for exactly permits, and returns the contract its host signs once the
// request is approved, waiting up to Wait.
func Request(ctx *astral.Context, c *astrald.Client, permits ...*auth.Permit) (*auth.SignedContract, error) {
	defer Waiting()()
	wctx, cancel := ctx.WithTimeout(Wait)
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

	var contract *auth.SignedContract
	err = ch.Switch(
		func(*astral.ObjectID) error { return nil },
		func(sc *auth.SignedContract) error { contract = sc; return channel.ErrBreak },
		channel.PassErrors,
		channel.WithContext(wctx),
		channel.FailOnEOF(),
	)
	if contract != nil {
		return contract, nil
	}
	if err == nil {
		err = errors.New("no contract")
	}
	return nil, err
}
