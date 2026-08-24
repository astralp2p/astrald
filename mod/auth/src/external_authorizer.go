package auth

import (
	"errors"
	"fmt"
	"strings"

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

// addExternalAuthorizers registers one authorizer per configured action type.
// It is what the loader calls, and what an op that registers an authority at
// runtime would call.
func (mod *Module) addExternalAuthorizers(configs []ExternalConfig) error {
	for _, config := range configs {
		switch {
		case config.Endpoint == "":
			return errors.New("external authorizer: no endpoint named")
		case len(config.Actions) == 0:
			return fmt.Errorf("external authorizer %v: no actions named", config.Endpoint)
		}

		ask, err := newAuthorizeAsk(mod, config)
		if err != nil {
			return fmt.Errorf("external authorizer: %w", err)
		}

		for _, action := range config.Actions {
			if err = mod.AddExternal(action, config, ask); err != nil {
				return err
			}
		}
	}

	return nil
}

// AddExternal binds an action type to an authority.
//
// why a second is refused rather than appended: two authorities over one action
// makes the decision an OR across them, and an authority that can be outvoted
// is not one.
func (mod *Module) AddExternal(action string, config ExternalConfig, ask auth.AuthorizeAsk) error {
	h := NewExternalAuthorizer(mod.log, action, config, ask)

	if _, added := mod.external.Set(action, h); !added {
		return fmt.Errorf("external authorizer: %v is named by two authorities", action)
	}

	return nil
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
