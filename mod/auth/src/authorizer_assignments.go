package auth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/lib/arl"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// authorizersPath is where the assignments persist.
const authorizersPath = "/mod/auth/authorizers"

// assignments returns a copy of the persisted assignments.
func (mod *Module) assignments() []*auth.AuthorizerAssignment {
	set := mod.authorizers.Get()
	if set == nil {
		return nil
	}
	return append([]*auth.AuthorizerAssignment(nil), set.Assignments...)
}

// sameKey reports whether a and b name the same action and actor.
func sameKey(a, b *auth.AuthorizerAssignment) bool {
	return externalKey(string(a.Action), a.Actor) == externalKey(string(b.Action), b.Actor)
}

// withAssignment returns list with a in place of the entry of the same key.
func withAssignment(list []*auth.AuthorizerAssignment, a *auth.AuthorizerAssignment) []*auth.AuthorizerAssignment {
	next := withoutKey(list, a)
	return append(next, a)
}

// withoutKey returns list without the entry of a's key.
func withoutKey(list []*auth.AuthorizerAssignment, a *auth.AuthorizerAssignment) []*auth.AuthorizerAssignment {
	var next []*auth.AuthorizerAssignment
	for _, e := range list {
		if !sameKey(e, a) {
			next = append(next, e)
		}
	}
	return next
}

// SetAuthorizer persists the assignment, replacing the one with the same action
// and actor, then adds its authorizer. A failure to build the authorizer or to
// persist leaves both the tree and the registry unchanged.
func (mod *Module) SetAuthorizer(ctx *astral.Context, a *auth.AuthorizerAssignment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := mod.refuseSelfAuthority(a); err != nil {
		return err
	}

	config := assignmentConfig(a)
	ask, err := newAuthorizeAsk(mod, config)
	if err != nil {
		return fmt.Errorf("external authorizer: %w", err)
	}

	mod.authorizersMu.Lock()
	defer mod.authorizersMu.Unlock()

	next := withAssignment(mod.assignments(), a)
	if err = mod.authorizers.Set(ctx, &authmod.AuthorizerAssignments{Assignments: next}); err != nil {
		return err
	}

	mod.AddExternalAuthorizer(string(a.Action), a.Actor, config, ask)

	return nil
}

// RemoveAuthorizer removes the assignment for the action and actor from the
// tree, then from the registry. Removing an absent assignment succeeds.
func (mod *Module) RemoveAuthorizer(ctx *astral.Context, action string, actor *astral.Identity) error {
	mod.authorizersMu.Lock()
	defer mod.authorizersMu.Unlock()

	key := &auth.AuthorizerAssignment{Action: astral.String8(action), Actor: actor}
	next := withoutKey(mod.assignments(), key)
	if err := mod.authorizers.Set(ctx, &authmod.AuthorizerAssignments{Assignments: next}); err != nil {
		return err
	}

	mod.RemoveExternalAuthorizer(action, actor)

	return nil
}

// refuseSelfAuthority refuses an astral endpoint that names the actor.
//
// why: an authority asked whether it may act would approve itself. The asker
// refuses this at question time too; refusing here reports it to whoever set it.
func (mod *Module) refuseSelfAuthority(a *auth.AuthorizerAssignment) error {
	if a.Actor == nil || a.Actor.IsZero() || !strings.HasPrefix(string(a.Endpoint), astralScheme) {
		return nil
	}

	_, target, _ := arl.Split(strings.TrimPrefix(string(a.Endpoint), astralScheme))
	id, err := mod.Dir.ResolveIdentity(target)
	if err != nil {
		return fmt.Errorf("resolve authority %v: %w", target, err)
	}
	if id.IsEqual(a.Actor) {
		return errors.New("the authority cannot be the actor")
	}

	return nil
}

// restoreAuthorizers writes the configured authorizers into the tree, replacing
// the assignments of the same key, then adds every persisted assignment.
//
// why the configuration wins on every start: it is the operator's file, and a
// runtime change to an action it names lasts until the next start.
//
// note: a persisted assignment whose authorizer cannot be built is logged and
// skipped; a skipped authorizer permits nothing.
func (mod *Module) restoreAuthorizers(ctx *astral.Context, configured []*auth.AuthorizerAssignment) error {
	mod.authorizersMu.Lock()
	defer mod.authorizersMu.Unlock()

	list := mod.assignments()
	for _, a := range configured {
		list = withAssignment(list, a)
	}

	if len(configured) > 0 {
		if err := mod.authorizers.Set(ctx, &authmod.AuthorizerAssignments{Assignments: list}); err != nil {
			return err
		}
	}

	for _, a := range list {
		if err := mod.addAssignment(a); err != nil {
			mod.log.Error("skipping authorizer for %v: %v", a.Action, err)
		}
	}

	return nil
}
