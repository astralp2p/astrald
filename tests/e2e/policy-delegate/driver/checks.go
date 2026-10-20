package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

const (
	// routeBound is the router's en-route bound for a query (core/router.go,
	// routingTimeout).
	routeBound = 60 * time.Second
	// why 70 s: 10 s past the route bound proves the registration outlives it
	// and keeps the suite short.
	holdPastRouteBound = 70 * time.Second
	decisionTimeout    = 5 * time.Second
	// why 25 s: a loaded host stalls the node for seconds, and a refusal
	// within 30 s, well below the 60 s route bound and the 10 min default
	// bound, shows the 5 s bound applied.
	decisionTimeoutSlack = 25 * time.Second
)

func setPolicy(ctx *astral.Context, admin *astrald.Client, args query.Args) error {
	return ack(admin.QueryChannel(ctx, "apphost.set_app_register_policy", args))
}

func delegateTo(cp *controlPlane, timeout string) query.Args {
	args := query.Args{"identity": cp.id.String(), "operation": opDecide}
	if timeout != "" {
		args["timeout"] = timeout
	}
	return args
}

// checkHold: a delegate that holds the registration past the router's bound
// still delivers the token.
func checkHold(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) error {
	cp.set(holdPastRouteBound, nil)
	if err := setPolicy(ctx, admin, delegateTo(cp, "")); err != nil {
		return fmt.Errorf("hold: set policy: %w", err)
	}
	t0 := time.Now()
	tok, err := apphostcli.New(nil, admin).Register(ctx)
	elapsed := time.Since(t0)
	if err != nil {
		return fmt.Errorf("hold: registration held %v failed after %v: %w", holdPastRouteBound, elapsed.Round(time.Second), err)
	}
	if _, err := await(cp.received, time.Second, "the delegate's request"); err != nil {
		return fmt.Errorf("hold: %w", err)
	}
	ended, err := await(cp.ended, 5*time.Second, "the delegate's wait to end")
	if err != nil {
		return fmt.Errorf("hold: %w", err)
	}
	held := tok.Identity != nil && tok.Token != "" && elapsed > routeBound && strings.HasPrefix(ended, "held")
	facts["hold_delivered_token"] = held
	if !held {
		return fmt.Errorf("hold: token after %v, delegate %q; want a token after more than %v", elapsed.Round(time.Second), ended, routeBound)
	}
	step("hold: token for %v after %v; delegate %s", tok.Identity, elapsed.Round(time.Second), ended)
	return nil
}

// checkDisconnect: a registering client that leaves ends the delegate's
// question within seconds and provisions no token.
func checkDisconnect(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) error {
	cp.set(10*time.Minute, nil)
	before, err := countTokens(ctx, admin)
	if err != nil {
		return err
	}
	ch, err := admin.QueryChannel(ctx, apphostapi.MethodRegister, nil)
	if err != nil {
		return fmt.Errorf("disconnect: register: %w", err)
	}
	if _, err := await(cp.received, 15*time.Second, "the delegate's request"); err != nil {
		ch.Close()
		return fmt.Errorf("disconnect: %w", err)
	}
	t0 := time.Now()
	ch.Close()
	ended, err := await(cp.ended, 10*time.Second, "the delegate's wait to end after the client left")
	endedAfter := time.Since(t0)
	hungUp := err == nil && strings.HasPrefix(ended, "node hung up")
	facts["disconnect_ended_question"] = hungUp
	if !hungUp {
		return fmt.Errorf("disconnect: the delegate's wait ended with %q (%v); want the node hanging up", ended, err)
	}
	// why: a token provisioned late shows up only after the node has
	// handled the hang-up.
	time.Sleep(2 * time.Second)
	after, err := countTokens(ctx, admin)
	if err != nil {
		return err
	}
	facts["disconnect_provisioned_nothing"] = after == before
	if after != before {
		return fmt.Errorf("disconnect: tokens went from %d to %d after the client left", before, after)
	}
	step("disconnect: question ended %v after the client left (%s); tokens stayed at %d", endedAfter.Round(100*time.Millisecond), ended, after)
	return nil
}

// checkTimeout: the setter refuses a bound above its maximum, and a delegate
// set with a 5 s bound refuses a registration it never answers after 5 s.
func checkTimeout(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) error {
	err := setPolicy(ctx, admin, delegateTo(cp, "11m"))
	facts["setter_refused_11m"] = err != nil
	if err == nil {
		return errors.New("timeout: the setter accepted timeout=11m")
	}
	step("timeout: timeout=11m refused by the setter: %v", err)

	if err := setPolicy(ctx, admin, delegateTo(cp, decisionTimeout.String())); err != nil {
		return fmt.Errorf("timeout: set policy with timeout=%v: %w", decisionTimeout, err)
	}
	cp.set(10*time.Minute, nil)
	t0 := time.Now()
	_, err = apphostcli.New(nil, admin).Register(ctx)
	elapsed := time.Since(t0)
	refused := err != nil && elapsed >= decisionTimeout && elapsed < decisionTimeout+decisionTimeoutSlack
	facts["timeout_refused_after_5s"] = refused
	if !refused {
		return fmt.Errorf("timeout: registration ended after %v with %v; want a refusal after %v", elapsed.Round(100*time.Millisecond), err, decisionTimeout)
	}
	if _, err := await(cp.received, time.Second, "the delegate's request"); err != nil {
		return fmt.Errorf("timeout: %w", err)
	}
	ended, _ := await(cp.ended, 10*time.Second, "the delegate's wait to end after the bound")
	step("timeout: refused after %v (%v); delegate %s", elapsed.Round(100*time.Millisecond), err, ended)
	return nil
}

// checkDecisionAuthorizer: a decision naming an authorizer records it with
// the new identity as actor, and the app's next action asks it.
func checkDecisionAuthorizer(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) error {
	action := auth.AdminManageAppsAction{}.ObjectType()
	cp.set(0, []*auth.AuthorizerAssignment{{Action: astral.String8(action), Endpoint: astral.String8(cp.endpoint())}})
	if err := setPolicy(ctx, admin, delegateTo(cp, "")); err != nil {
		return fmt.Errorf("authorizer: set policy: %w", err)
	}
	tok, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return fmt.Errorf("authorizer: register through a delegate naming an authorizer: %w", err)
	}
	_, _ = await(cp.received, time.Second, "the delegate's request")
	_, _ = await(cp.ended, time.Second, "the delegate's answer")
	// why: the assignment names the new app; removing it leaves the node as
	// the check found it.
	defer removeAuthorizer(ctx, admin, action, tok.Identity.String())

	list, err := listAuthorizers(ctx, admin)
	if err != nil {
		return fmt.Errorf("authorizer: list: %w", err)
	}
	got := find(list, action, tok.Identity)
	recorded := got != nil && string(got.Endpoint) == cp.endpoint()
	facts["decision_authorizer_recorded"] = recorded
	if !recorded {
		return fmt.Errorf("authorizer: no assignment of %v to %v for actor %v; have %v", action, cp.endpoint(), tok.Identity, describeAll(list))
	}
	step("authorizer: registration recorded %v", describe(got))
	asked, err := checkAsked(ctx, cp, tok)
	facts["decision_authorizer_asked"] = asked
	return err
}

// checkAsked runs the new app's apphost.list_tokens, which needs
// AdminManageApps, and reports whether the named authorizer was asked about
// the new identity.
func checkAsked(ctx *astral.Context, cp *controlPlane, tok *apphostapi.AccessToken) (bool, error) {
	if _, err := countTokens(ctx, connect(cp.nodeEndpoint, string(tok.Token))); err != nil {
		return false, fmt.Errorf("authorizer: the new app's apphost.list_tokens was refused: %w", err)
	}
	want := auth.AdminManageAppsAction{}.ObjectType() + " actor=" + tok.Identity.String()
	asked, err := await(cp.asked, 5*time.Second, "the authorizer to be asked")
	if err != nil || asked != want {
		return false, fmt.Errorf("authorizer: asked %q (%v); want %q", asked, err, want)
	}
	step("authorizer: the new app's action reached the named authorizer")
	return true, nil
}
