package astrald

import (
	"github.com/cryptopunkscc/astral-go/astral/sig"
	libapphost "github.com/cryptopunkscc/astrald/lib/apphost"
)

// SetRetry wraps the default client's router in a RetryRouter with the given retry policy.
// This affects all outbound queries made via Default(), including all mod/*/client packages.
func SetRetry(r *sig.Retry) {
	SetDefault(New(NewRetryRouter(libapphost.DefaultRouter(), r)))
}
