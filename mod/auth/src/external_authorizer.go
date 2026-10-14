package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

var _ authmod.TypedHandler = &ExternalAuthorizer{}

// ExternalAuthorizer answers one action type by asking an authority outside the
// node. How it asks is the auth.AuthorizeAsk it holds; what it does with the
// answer is the same either way.
//
// note: nothing is remembered between questions. Every authorization of a
// configured action type puts one question to the authority, and a contract
// chain puts one per level.
type ExternalAuthorizer struct {
	log    *log.Logger
	config ExternalConfig
	action string
	ask    auth.AuthorizeAsk
}

// NewExternalAuthorizer builds the authorizer for one action type over an ask.
// The ask is the astral-go interface, so an authority reached by means this
// module knows nothing about is registered the same way a configured one is.
func NewExternalAuthorizer(
	logger *log.Logger,
	action string,
	config ExternalConfig,
	ask auth.AuthorizeAsk,
) *ExternalAuthorizer {
	return &ExternalAuthorizer{
		log:    logger,
		config: config.withDefaults(),
		action: action,
		ask:    ask,
	}
}

// newAuthorizeAsk selects the means from the endpoint's scheme.
func newAuthorizeAsk(mod *Module, config ExternalConfig) (auth.AuthorizeAsk, error) {
	switch {
	case strings.HasPrefix(config.Endpoint, "http://"),
		strings.HasPrefix(config.Endpoint, "https://"):
		return newHTTPAuthorizer(config)

	case strings.HasPrefix(config.Endpoint, astralScheme):
		return newAstralAuthorizer(mod, config)

	default:
		return nil, fmt.Errorf(
			"endpoint %v names no transport: expected http://, https:// or %v",
			config.Endpoint, astralScheme,
		)
	}
}

// configAssignments turns the configured authorizers into one action-wide
// assignment per named action.
//
// why a second authority for one action is refused rather than appended: two
// authorities over one action makes the decision an OR across them, and an
// authority that can be outvoted is not one.
func configAssignments(configs []ExternalConfig) ([]*auth.AuthorizerAssignment, error) {
	var list []*auth.AuthorizerAssignment
	named := map[string]bool{}

	for _, config := range configs {
		if len(config.Actions) == 0 {
			return nil, fmt.Errorf("external authorizer %v: no actions named", config.Endpoint)
		}

		for _, action := range config.Actions {
			if named[action] {
				return nil, fmt.Errorf("external authorizer: %v is named by two authorities", action)
			}
			named[action] = true

			a := &auth.AuthorizerAssignment{
				Action:     astral.String8(action),
				Endpoint:   astral.String8(config.Endpoint),
				Timeout:    astral.Duration(config.Timeout),
				TokenFile:  astral.String8(config.TokenFile),
				TokenEnv:   astral.String8(config.TokenEnv),
				TokenValue: astral.String8(config.TokenValue),
			}
			if err := a.Validate(); err != nil {
				return nil, fmt.Errorf("external authorizer: %w", err)
			}
			list = append(list, a)
		}
	}

	return list, nil
}

// assignmentConfig is the ExternalConfig an assignment describes.
func assignmentConfig(a *auth.AuthorizerAssignment) ExternalConfig {
	return ExternalConfig{
		Endpoint:   string(a.Endpoint),
		TokenValue: string(a.TokenValue),
		TokenFile:  string(a.TokenFile),
		TokenEnv:   string(a.TokenEnv),
		Timeout:    time.Duration(a.Timeout),
	}
}

// addAssignment builds the authorizer an assignment names and adds it.
func (mod *Module) addAssignment(a *auth.AuthorizerAssignment) error {
	config := assignmentConfig(a)

	ask, err := newAuthorizeAsk(mod, config)
	if err != nil {
		return fmt.Errorf("external authorizer: %w", err)
	}

	mod.AddExternalAuthorizer(string(a.Action), a.Actor, config, ask)

	return nil
}

// externalKey is the registry key of an assignment. A nil or zero actor makes
// it action-wide.
func externalKey(action string, actor *astral.Identity) string {
	if actor == nil || actor.IsZero() {
		return action
	}
	return action + "/" + actor.String()
}

// AddExternalAuthorizer binds an action type to an authority, for one actor or,
// with a nil actor, for every actor. It replaces the authority bound to the same
// action and actor.
//
// The ask is the astral-go interface, so an authority reached by means this
// module knows nothing about is added the same way an assigned one is.
func (mod *Module) AddExternalAuthorizer(action string, actor *astral.Identity, config ExternalConfig, ask auth.AuthorizeAsk) {
	mod.external.Replace(externalKey(action, actor), NewExternalAuthorizer(mod.log, action, config, ask))
}

// RemoveExternalAuthorizer unbinds the authority for the action and actor.
func (mod *Module) RemoveExternalAuthorizer(action string, actor *astral.Identity) {
	mod.external.Delete(externalKey(action, actor))
}

// externalFor returns the authority that answers for actor at this level: the
// actor's own assignment when one names the action type, otherwise the
// action-wide one.
//
// why the actor's wins: it is the narrower statement.
func (mod *Module) externalFor(actor *astral.Identity, actionType string) (*ExternalAuthorizer, bool) {
	if h, ok := mod.external.Get(externalKey(actionType, actor)); ok {
		return h, true
	}
	return mod.external.Get(actionType)
}

func (h *ExternalAuthorizer) ActionType() string { return h.action }

// Authorize puts the question to the authority.
//
// why a failure refuses: a question that could not be put is not an answer, and
// an authority that cannot be reached has permitted nothing.
func (h *ExternalAuthorizer) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	ctx, cancel := ctx.WithTimeout(h.config.Timeout)
	defer cancel()

	allow, err := h.ask.Ask(ctx, action)
	if err != nil {
		h.log.Errorv(1, "external authorizer %v via %v: %v", h.action, h.ask, err)
		return false
	}

	return allow
}
