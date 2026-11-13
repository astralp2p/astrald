package auth

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// NewAuthorizeAsk builds the asker an external authorizer uses for the endpoint, bounded by timeout; see
// auth.Module.
func (mod *Module) NewAuthorizeAsk(endpoint string, timeout time.Duration) (auth.AuthorizeAsk, error) {
	config := ExternalConfig{Endpoint: endpoint, Timeout: timeout}.withDefaults()

	ask, err := newAuthorizeAsk(mod, config)
	if err != nil {
		return nil, err
	}

	return &boundedAsk{ask: ask, timeout: config.Timeout}, nil
}

// boundedAsk bounds every question of an asker by a timeout, as ExternalAuthorizer.Authorize does.
type boundedAsk struct {
	ask     auth.AuthorizeAsk
	timeout time.Duration
}

func (b *boundedAsk) Ask(ctx *astral.Context, action auth.ActionObject) (bool, error) {
	ctx, cancel := ctx.WithTimeout(b.timeout)
	defer cancel()

	return b.ask.Ask(ctx, action)
}

func (b *boundedAsk) String() string {
	if s, ok := b.ask.(interface{ String() string }); ok {
		return s.String()
	}

	return "bounded asker"
}
