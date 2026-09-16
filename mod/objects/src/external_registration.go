package objects

import (
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// renewable is the part of externalLease the renewal path needs. Declared where
// it is used so an internal provider, which does not implement it, is never
// mistaken for a registration that can be renewed.
type renewable interface {
	renew(now time.Time, d time.Duration) time.Time
}

// grantLease decides the lease the node is willing to give for a requested one.
// A request of zero takes the node's default; anything above the node's maximum
// is clamped down to it rather than refused, so a registrant that asks for too
// much still registers and learns what it actually got.
func (mod *Module) grantLease(requested time.Duration) time.Duration {
	max := mod.config.MaxExternalRegistrationLease
	if max <= 0 {
		max = defaultConfig.MaxExternalRegistrationLease
	}

	if requested <= 0 {
		requested = mod.config.ExternalRegistrationLease
	}
	if requested <= 0 {
		requested = defaultConfig.ExternalRegistrationLease
	}

	if requested > max {
		return max
	}

	return requested
}

// renewExternal renews the lease of the entry registered by id, reporting the
// new expiry and whether such an entry was there to renew.
//
// Callers hold mod.externalMu: finding no entry here is what makes the caller
// add one, and without the lock two concurrent registrations by the same
// identity would both find nothing and both add.
func renewExternal[T comparable](set *sig.Set[T], id *astral.Identity, now time.Time, d time.Duration) (time.Time, bool) {
	for _, item := range set.Clone() {
		source, ok, err := objectsmod.SourceIdentity(item)
		if err != nil || !ok {
			continue
		}

		if !source.IsEqual(id) {
			continue
		}

		leased, ok := any(item).(renewable)
		if !ok {
			// note: an internal provider registered under this identity holds no lease.
			// It is not ours to renew and not ours to replace.
			continue
		}

		return leased.renew(now, d), true
	}

	return time.Time{}, false
}

func newRegistrationLease(granted time.Duration, expiresAt time.Time) *objects.RegistrationLease {
	return &objects.RegistrationLease{
		Duration:  astral.Duration(granted),
		ExpiresAt: astral.Time(expiresAt),
	}
}

// registerExternalDescriber registers id as an external describer, or renews the
// registration it already holds, and reports the lease granted either way.
func (mod *Module) registerExternalDescriber(id *astral.Identity, requested time.Duration) (*objects.RegistrationLease, error) {
	granted := mod.grantLease(requested)
	now := time.Now()

	mod.externalMu.Lock()
	defer mod.externalMu.Unlock()

	if expiresAt, ok := renewExternal(&mod.describers, id, now, granted); ok {
		return newRegistrationLease(granted, expiresAt), nil
	}

	lease := newExternalLease(now, granted)

	// why: AddDescriber is not used here because it takes externalMu, already
	// held, and its same-identity check is the one renewExternal just did.
	if err := mod.describers.Add(NewExternalDescriber(mod, id, lease)); err != nil {
		return nil, err
	}

	return newRegistrationLease(granted, lease.expiry()), nil
}

// registerExternalSearcher registers id as an external searcher, or renews the
// registration it already holds, and reports the lease granted either way.
func (mod *Module) registerExternalSearcher(id *astral.Identity, requested time.Duration) (*objects.RegistrationLease, error) {
	granted := mod.grantLease(requested)
	now := time.Now()

	mod.externalMu.Lock()
	defer mod.externalMu.Unlock()

	if expiresAt, ok := renewExternal(&mod.searchers, id, now, granted); ok {
		return newRegistrationLease(granted, expiresAt), nil
	}

	lease := newExternalLease(now, granted)

	if err := mod.searchers.Add(NewExternalSearcher(mod, id, lease)); err != nil {
		return nil, err
	}

	return newRegistrationLease(granted, lease.expiry()), nil
}

// registerExternalFinder registers id as an external finder, or renews the
// registration it already holds, and reports the lease granted either way.
func (mod *Module) registerExternalFinder(id *astral.Identity, requested time.Duration) (*objects.RegistrationLease, error) {
	granted := mod.grantLease(requested)
	now := time.Now()

	mod.externalMu.Lock()
	defer mod.externalMu.Unlock()

	if expiresAt, ok := renewExternal(&mod.finders, id, now, granted); ok {
		return newRegistrationLease(granted, expiresAt), nil
	}

	lease := newExternalLease(now, granted)

	if err := mod.finders.Add(NewExternalFinder(mod, id, lease)); err != nil {
		return nil, err
	}

	return newRegistrationLease(granted, lease.expiry()), nil
}

// sweepExternalRegistrations removes expired external registrations until ctx
// ends.
//
// The fan-out already skips an expired entry, so this frees memory and records
// the expiry rather than deciding correctness: an expired registration stops
// being called the moment it expires, whatever the sweep's timing.
func (mod *Module) sweepExternalRegistrations(ctx *astral.Context) {
	interval := mod.config.ExternalRegistrationSweepInterval
	if interval <= 0 {
		interval = defaultConfig.ExternalRegistrationSweepInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mod.removeExpiredRegistrations(time.Now())
		}
	}
}

// removeExpiredRegistrations drops every external registration whose lease has
// run out and logs each one.
func (mod *Module) removeExpiredRegistrations(now time.Time) {
	for _, describer := range mod.describers.Clone() {
		external, ok := describer.(*ExternalDescriber)
		if !ok || !external.expired(now) {
			continue
		}

		// note: a concurrent call can remove the same describer first.
		if err := mod.describers.Remove(describer); err != nil {
			continue
		}

		mod.log.Log("external describer %v: registration expired", external.id)
	}

	for _, searcher := range mod.searchers.Clone() {
		external, ok := searcher.(*ExternalSearcher)
		if !ok || !external.expired(now) {
			continue
		}

		if err := mod.searchers.Remove(searcher); err != nil {
			continue
		}

		mod.log.Log("external searcher %v: registration expired", external.id)
	}

	for _, finder := range mod.finders.Clone() {
		external, ok := finder.(*ExternalFinder)
		if !ok || !external.expired(now) {
			continue
		}

		if err := mod.finders.Remove(finder); err != nil {
			continue
		}

		mod.log.Log("external finder %v: registration expired", external.id)
	}
}
