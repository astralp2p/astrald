package objects

import (
	"sync"
	"time"
)

// externalLease is the expiry an external registration carries. An external
// describer, searcher or finder holds one; internal providers hold none and
// therefore never expire.
//
// The zero value is not usable: newExternalLease sets the first expiry, because
// a lease that starts at the zero time is already expired.
type externalLease struct {
	mu        sync.Mutex
	expiresAt time.Time
}

func newExternalLease(now time.Time, d time.Duration) *externalLease {
	return &externalLease{expiresAt: now.Add(d)}
}

// renew moves the expiry to now+d and reports it. Renewing an expired lease
// revives it: the registrant proved it is still there by asking, and the sweep
// may not have removed the entry yet, so refusing here would depend on sweep
// timing.
func (l *externalLease) renew(now time.Time, d time.Duration) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.expiresAt = now.Add(d)

	return l.expiresAt
}

func (l *externalLease) expired(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return !now.Before(l.expiresAt)
}

func (l *externalLease) expiry() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.expiresAt
}

// leasedProvider is what the fan-out and the sweep test a registered provider
// for. An internal provider does not implement it and is never skipped or
// removed for expiry.
type leasedProvider interface {
	expired(now time.Time) bool
}

// expiredProvider reports whether a registered provider holds a lease that has
// run out. A provider with no lease never has.
func expiredProvider(provider any, now time.Time) bool {
	leased, ok := provider.(leasedProvider)

	return ok && leased.expired(now)
}
