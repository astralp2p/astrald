package apps

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	apphostclient "github.com/cryptopunkscc/astrald/mod/apphost/client"
)

// NodeBind opens a bind channel to the node.
type NodeBind func(ctx *astral.Context) (*channel.Channel, error)

func DefaultNodeBind() NodeBind { return apphostclient.Default().Bind }

// WithRetry sets the default exponential backoff on the node bind, silently.
func WithRetry() AppRegistrarOption {
	r, _ := sig.NewRetry(1*time.Second, 30*time.Second, 2)
	return WithBind(RetryBind(DefaultNodeBind(), r, nil))
}

// WithRetryCallback sets the default exponential backoff on the node bind, calling onRetry on each attempt.
func WithRetryCallback(onRetry func(attempt int, err error)) AppRegistrarOption {
	r, _ := sig.NewRetry(1*time.Second, 30*time.Second, 2)
	return WithBind(RetryBind(DefaultNodeBind(), r, onRetry))
}

// RetryBind wraps a NodeBind with exponential backoff. onRetry is optional.
func RetryBind(base NodeBind, r *sig.Retry, onRetry func(attempt int, err error)) NodeBind {
	return func(ctx *astral.Context) (*channel.Channel, error) {
		for attempt := 0; ; attempt++ {
			if ch, err := base(ctx); err == nil {
				r.Reset()
				return ch, nil
			} else if onRetry != nil {
				onRetry(attempt, err)
			}
			select {
			case <-r.Retry():
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}
