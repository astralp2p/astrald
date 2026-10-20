package main

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// authzState names the two assignments the driver sets and the oracle finds
// after the restart. The fields are facts in session.json.
type authzState struct {
	Action   string `json:"authorizer_action"`
	Endpoint string `json:"authorizer_endpoint"`
	Actor    string `json:"authorizer_actor"`
}

func setAuthorizer(ctx *astral.Context, admin *astrald.Client, st authzState, actor string) error {
	args := query.Args{"action": st.Action, "endpoint": st.Endpoint}
	if actor != "" {
		args["actor"] = actor
	}
	return ack(admin.QueryChannel(ctx, "auth.set_authorizer", args))
}

func removeAuthorizer(ctx *astral.Context, admin *astrald.Client, action, actor string) error {
	args := query.Args{"action": action}
	if actor != "" {
		args["actor"] = actor
	}
	return ack(admin.QueryChannel(ctx, "auth.remove_authorizer", args))
}

// setAuthorizers sets an action-wide and an actor-specific authorizer and
// records both as facts for the oracle.
func setAuthorizers(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) (err error) {
	st := authzState{
		Action:   auth.SeeObjectsAction{}.ObjectType(),
		Endpoint: cp.endpoint(),
		Actor:    astral.GenerateIdentity().String(),
	}
	// why: on failure the oracle never runs, and an action-wide assignment
	// to this driver's app, which exits with the driver, outlives the test.
	defer func() {
		if err != nil {
			_ = removeAuthorizer(ctx, admin, st.Action, st.Actor)
			_ = removeAuthorizer(ctx, admin, st.Action, "")
		}
	}()
	if err := setAuthorizer(ctx, admin, st, ""); err != nil {
		return fmt.Errorf("set the action-wide authorizer: %w", err)
	}
	if err := setAuthorizer(ctx, admin, st, st.Actor); err != nil {
		return fmt.Errorf("set the actor-specific authorizer: %w", err)
	}
	if err := expectListed(ctx, admin, st, presence{wide: true, specific: true}); err != nil {
		return fmt.Errorf("after set: %w", err)
	}
	facts["authorizers_set"] = true
	facts["authorizer_action"], facts["authorizer_endpoint"], facts["authorizer_actor"] = st.Action, st.Endpoint, st.Actor
	step("authorizers: an action-wide and an actor-specific %v assignment are listed", st.Action)
	return nil
}

// verifyAuthorizers checks both assignments survived the restart, then
// removes the actor-specific one and the action-wide one in turn.
func verifyAuthorizers(ctx *astral.Context, admin *astrald.Client, st authzState) (err error) {
	// why: a failed check leaves an action-wide assignment to an app that no
	// longer runs, and every later test on the node would meet it.
	defer func() {
		if err != nil {
			_ = removeAuthorizer(ctx, admin, st.Action, st.Actor)
			_ = removeAuthorizer(ctx, admin, st.Action, "")
		}
	}()
	if err := expectListed(ctx, admin, st, presence{wide: true, specific: true}); err != nil {
		return fmt.Errorf("after the restart: %w", err)
	}
	step("authorizers: both assignments listed after the restart")

	if err := removeAuthorizer(ctx, admin, st.Action, st.Actor); err != nil {
		return fmt.Errorf("remove the actor-specific authorizer: %w", err)
	}
	if err := expectListed(ctx, admin, st, presence{wide: true}); err != nil {
		return fmt.Errorf("after removing the actor-specific authorizer: %w", err)
	}
	step("authorizers: the actor-specific assignment is removed; the action-wide one stays")

	if err := removeAuthorizer(ctx, admin, st.Action, ""); err != nil {
		return fmt.Errorf("remove the action-wide authorizer: %w", err)
	}
	if err := expectListed(ctx, admin, st, presence{}); err != nil {
		return fmt.Errorf("after removing the action-wide authorizer: %w", err)
	}
	step("authorizers: the action-wide assignment is removed")
	return nil
}

// presence is which of the two assignments auth.list_authorizers shows.
type presence struct{ wide, specific bool }

// expectListed checks that auth.list_authorizers shows the assignments want names.
func expectListed(ctx *astral.Context, admin *astrald.Client, st authzState, want presence) error {
	actor, err := astral.ParseIdentity(st.Actor)
	if err != nil {
		return err
	}
	list, err := listAuthorizers(ctx, admin)
	if err != nil {
		return err
	}
	w, s := find(list, st.Action, nil), find(list, st.Action, actor)
	if got := (presence{wide: w != nil, specific: s != nil}); got != want {
		return fmt.Errorf("listed %+v, want %+v; have %v", got, want, describeAll(list))
	}
	for _, a := range []*auth.AuthorizerAssignment{w, s} {
		if a != nil && string(a.Endpoint) != st.Endpoint {
			return errors.New("an assignment names endpoint " + string(a.Endpoint) + ", want " + st.Endpoint)
		}
	}
	return nil
}

func listAuthorizers(ctx *astral.Context, admin *astrald.Client) ([]*auth.AuthorizerAssignment, error) {
	objs, err := drain(ctx, admin, "auth.list_authorizers")
	if err != nil {
		return nil, err
	}
	var out []*auth.AuthorizerAssignment
	for _, o := range objs {
		if a, ok := o.(*auth.AuthorizerAssignment); ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// find returns the assignment of action to actor; a nil actor finds the
// action-wide one.
func find(list []*auth.AuthorizerAssignment, action string, actor *astral.Identity) *auth.AuthorizerAssignment {
	for _, a := range list {
		if string(a.Action) != action {
			continue
		}
		wide := a.Actor == nil || a.Actor.IsZero()
		if actor == nil && wide || actor != nil && !wide && a.Actor.IsEqual(actor) {
			return a
		}
	}
	return nil
}

func describe(a *auth.AuthorizerAssignment) string {
	actor := "*"
	if a.Actor != nil && !a.Actor.IsZero() {
		actor = a.Actor.String()
	}
	return fmt.Sprintf("%v actor=%v -> %v", a.Action, actor, a.Endpoint)
}

func describeAll(list []*auth.AuthorizerAssignment) []string {
	var out []string
	for _, a := range list {
		out = append(out, describe(a))
	}
	return out
}
