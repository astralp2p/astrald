package astrald

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	libapphost "github.com/cryptopunkscc/astrald/lib/apphost"
)

// RetryRouter wraps a Router and retries queries that fail with ErrNodeUnavailable,
// waiting between attempts according to the provided sig.Retry policy.
// maxAttempts == 0 means unlimited retries.
type RetryRouter struct {
	Router
	retry       *sig.Retry
	maxAttempts int // 0 = unlimited
}

var _ Router = &RetryRouter{}

func NewRetryRouter(r Router, retry *sig.Retry) *RetryRouter {
	return &RetryRouter{Router: r, retry: retry}
}

func NewLimitedRetryRouter(r Router, retry *sig.Retry, maxAttempts int) *RetryRouter {
	return &RetryRouter{Router: r, retry: retry, maxAttempts: maxAttempts}
}

func NewNoRetryRouter(r Router) *RetryRouter {
	return &RetryRouter{Router: r, maxAttempts: 1}
}

// RouteQuery retries the query on ErrNodeUnavailable; any other error is returned immediately.
// On success the retry policy is reset so the next call starts fresh.
func (rr *RetryRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery) (astral.Conn, error) {
	for attempt := 0; ; attempt++ {
		conn, err := rr.Router.RouteQuery(ctx, q)
		if err == nil {
			if rr.retry != nil {
				rr.retry.Reset()
			}
			return conn, nil
		}
		if !errors.Is(err, libapphost.ErrNodeUnavailable) {
			return nil, err
		}
		if rr.maxAttempts > 0 && attempt+1 >= rr.maxAttempts {
			return nil, err
		}
		select {
		case <-rr.retry.Retry():
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
