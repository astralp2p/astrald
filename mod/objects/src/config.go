package objects

import "time"

const (
	methodPut      = "objects.put"
	methodRead     = "objects.read"
	methodDescribe = "objects.describe"
	methodSearch   = "objects.search"
	methodPush     = "objects.push"
)

const DefaultRepoName = "default"

const (
	// MaxAlloc is the maximum allocatable storage space for an object
	MaxAlloc int64 = 1 << 40 //1TB; gomobile requires explicit int64 type.
)

type Config struct {
	DefaultMemSize int64

	// ExternalRegistrationLease is the lease granted to an external describer,
	// searcher or finder that asks for none.
	ExternalRegistrationLease time.Duration `yaml:"external_registration_lease,omitempty"`

	// MaxExternalRegistrationLease is the longest lease the node will grant. A
	// longer request is clamped to this rather than refused. It bounds how long a
	// registration made by a process that has since died can survive.
	MaxExternalRegistrationLease time.Duration `yaml:"max_external_registration_lease,omitempty"`

	// ExternalRegistrationSweepInterval is how often expired registrations are
	// removed. Discovery skips an expired registration whatever this is set to,
	// so it governs memory and logging rather than correctness.
	ExternalRegistrationSweepInterval time.Duration `yaml:"external_registration_sweep_interval,omitempty"`

	// PurgeUntouched is the window of the daily purge of the group local. 0 turns the daily purge off.
	PurgeUntouched time.Duration `yaml:"purge_untouched,omitempty"`
}

var defaultConfig = Config{
	ExternalRegistrationLease:         time.Hour,
	MaxExternalRegistrationLease:      6 * time.Hour,
	ExternalRegistrationSweepInterval: time.Minute,
	// note: 720 hours is the operator's decision of 2026-11-12, recorded in https://wiki.satforge.dev/doc/implement-the-vfs-module-and-the-changes-that-ship-with-it-oofFdNw03v
	PurgeUntouched: 720 * time.Hour,
}
