package objects

import (
	"testing"
	"time"
)

// TestGrantLeaseClampsAboveTheMaximum is the point of the node granting the
// lease rather than the registrant choosing it: a registrant asking for longer
// than the node allows is answered with what the node will give, not refused and
// not obeyed.
func TestGrantLeaseClampsAboveTheMaximum(t *testing.T) {
	mod := &Module{config: Config{
		ExternalRegistrationLease:    time.Hour,
		MaxExternalRegistrationLease: 6 * time.Hour,
	}}

	if got, want := mod.grantLease(24*time.Hour), 6*time.Hour; got != want {
		t.Fatalf("grantLease(24h): got %v, want the maximum %v", got, want)
	}
}

// TestGrantLeaseKeepsARequestUnderTheMaximum: the clamp is a ceiling, not a
// fixed lease, so a registrant that asks for less keeps what it asked for.
func TestGrantLeaseKeepsARequestUnderTheMaximum(t *testing.T) {
	mod := &Module{config: Config{
		ExternalRegistrationLease:    time.Hour,
		MaxExternalRegistrationLease: 6 * time.Hour,
	}}

	if got, want := mod.grantLease(30*time.Minute), 30*time.Minute; got != want {
		t.Fatalf("grantLease(30m): got %v, want %v", got, want)
	}
}

// TestGrantLeaseTakesTheDefaultForZero: the op's duration argument is optional,
// and a zero there means "the node decides" rather than a lease of no length.
func TestGrantLeaseTakesTheDefaultForZero(t *testing.T) {
	mod := &Module{config: Config{
		ExternalRegistrationLease:    time.Hour,
		MaxExternalRegistrationLease: 6 * time.Hour,
	}}

	if got, want := mod.grantLease(0), time.Hour; got != want {
		t.Fatalf("grantLease(0): got %v, want the default %v", got, want)
	}
}

// TestGrantLeaseFallsBackWhenUnconfigured: a node with no objects.yaml still
// grants a bounded lease, because an unset config must not read as an unbounded
// one — that is the state the change exists to end.
func TestGrantLeaseFallsBackWhenUnconfigured(t *testing.T) {
	mod := &Module{config: Config{}}

	if got, want := mod.grantLease(0), defaultConfig.ExternalRegistrationLease; got != want {
		t.Fatalf("grantLease(0) unconfigured: got %v, want %v", got, want)
	}

	if got, want := mod.grantLease(999*time.Hour), defaultConfig.MaxExternalRegistrationLease; got != want {
		t.Fatalf("grantLease(999h) unconfigured: got %v, want %v", got, want)
	}
}

// TestExternalLeaseExpiresAtItsExpiry covers the boundary the fan-out tests on
// every call: the instant the lease runs out it stops being valid, rather than
// staying valid for one more call.
func TestExternalLeaseExpiresAtItsExpiry(t *testing.T) {
	now := time.Now()
	lease := newExternalLease(now, time.Minute)

	if lease.expired(now) {
		t.Fatal("a lease just granted is expired")
	}
	if lease.expired(now.Add(59 * time.Second)) {
		t.Fatal("a lease is expired before its expiry")
	}
	if !lease.expired(now.Add(time.Minute)) {
		t.Fatal("a lease is not expired at its expiry")
	}
}

// TestExternalLeaseRenewMovesTheExpiry is renewal in miniature: the registrant
// repeating the op is what keeps its registration alive.
func TestExternalLeaseRenewMovesTheExpiry(t *testing.T) {
	now := time.Now()
	lease := newExternalLease(now, time.Minute)

	later := now.Add(30 * time.Second)
	if got, want := lease.renew(later, time.Minute), later.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("renew: got %v, want %v", got, want)
	}

	if lease.expired(now.Add(time.Minute)) {
		t.Fatal("a renewed lease still expires at its original expiry")
	}
}

// TestExpiredProviderIgnoresAProviderWithoutALease: internal providers are
// compiled into the node and hold no lease, so nothing here may drop them.
func TestExpiredProviderIgnoresAProviderWithoutALease(t *testing.T) {
	type internalDescriber struct{}

	if expiredProvider(&internalDescriber{}, time.Now()) {
		t.Fatal("a provider holding no lease is reported expired")
	}
}

// TestExpiredProviderReportsALapsedRegistration is the other half: a leased
// provider past its expiry is what the fan-out must skip.
func TestExpiredProviderReportsALapsedRegistration(t *testing.T) {
	now := time.Now()
	describer := &ExternalDescriber{lease: newExternalLease(now, time.Minute)}

	if expiredProvider(describer, now) {
		t.Fatal("a live registration is reported expired")
	}
	if !expiredProvider(describer, now.Add(2*time.Minute)) {
		t.Fatal("a lapsed registration is not reported expired")
	}
}
