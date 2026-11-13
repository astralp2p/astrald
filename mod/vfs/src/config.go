package vfs

import (
	"time"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

const (
	defaultByteLimit  uint64 = 1 << 30
	defaultInodeLimit uint64 = 1_000_000
)

type Config struct {
	// ByteLimit is the byte limit of a new VFS. 0 means no limit.
	ByteLimit uint64 `yaml:"byte_limit"`

	// InodeLimit is the inode limit of a new VFS. 0 means no limit.
	InodeLimit uint64 `yaml:"inode_limit"`

	// DirectoryQuietTime is how long a VFS stays unchanged before the background compute stores its directories.
	DirectoryQuietTime time.Duration `yaml:"directory_quiet_time"`

	// DirectoryLongestWait is how long a VFS waits at most for the background compute, even while it keeps changing.
	DirectoryLongestWait time.Duration `yaml:"directory_longest_wait"`

	// DirectoryBucketSize is the token bucket of one VFS in bytes of directory objects.
	DirectoryBucketSize uint64 `yaml:"directory_bucket_size"`

	// DirectoryRefillRate refills the bucket of one VFS, in bytes per second.
	DirectoryRefillRate uint64 `yaml:"directory_refill_rate"`

	// DirectoryMinCharge is the least a background compute is charged for one directory object it stores.
	DirectoryMinCharge uint64 `yaml:"directory_min_charge"`

	// DirectoryNodeRate bounds the background computes of all VFSes together, in bytes per second.
	DirectoryNodeRate uint64 `yaml:"directory_node_rate"`

	// DirectorySweepInterval is how often the sweeper deletes replaced directory objects.
	DirectorySweepInterval time.Duration `yaml:"directory_sweep_interval"`

	// DirectoryGraceTime is how long a replaced directory object stays before the sweeper deletes it.
	DirectoryGraceTime time.Duration `yaml:"directory_grace_time"`
}

var defaultConfig = Config{
	ByteLimit:              defaultByteLimit,
	InodeLimit:             defaultInodeLimit,
	DirectoryQuietTime:     5 * time.Second,
	DirectoryLongestWait:   60 * time.Second,
	DirectoryBucketSize:    8 << 20,
	DirectoryRefillRate:    4 << 10,
	DirectoryMinCharge:     4 << 10,
	DirectoryNodeRate:      32 << 10,
	DirectorySweepInterval: 10 * time.Minute,
	DirectoryGraceTime:     time.Hour,
}

// checkDirectoryValues replaces each directory value of 0 or below by its default, and a minimum charge above the
// bucket size by the defaults of both. Each replacement is logged.
//
// why: a zero rate or interval would stop the background compute or the sweeper.
// why: a minimum charge above the bucket would make every compute wait for a full bucket.
func (c *Config) checkDirectoryValues(l *log.Logger) {
	d := defaultConfig
	checkPositive(&c.DirectoryQuietTime, d.DirectoryQuietTime, "directory_quiet_time", l)
	checkPositive(&c.DirectoryLongestWait, d.DirectoryLongestWait, "directory_longest_wait", l)
	checkPositive(&c.DirectoryBucketSize, d.DirectoryBucketSize, "directory_bucket_size", l)
	checkPositive(&c.DirectoryRefillRate, d.DirectoryRefillRate, "directory_refill_rate", l)
	checkPositive(&c.DirectoryMinCharge, d.DirectoryMinCharge, "directory_min_charge", l)
	checkPositive(&c.DirectoryNodeRate, d.DirectoryNodeRate, "directory_node_rate", l)
	checkPositive(&c.DirectorySweepInterval, d.DirectorySweepInterval, "directory_sweep_interval", l)
	checkPositive(&c.DirectoryGraceTime, d.DirectoryGraceTime, "directory_grace_time", l)

	if c.DirectoryMinCharge > c.DirectoryBucketSize {
		l.Log("config directory_min_charge %v is above directory_bucket_size %v; both use their defaults",
			c.DirectoryMinCharge, c.DirectoryBucketSize)
		c.DirectoryMinCharge, c.DirectoryBucketSize = d.DirectoryMinCharge, d.DirectoryBucketSize
	}
}

// checkPositive replaces a value of 0 or below by its default and logs it.
func checkPositive[T ~int64 | ~uint64](v *T, def T, key string, l *log.Logger) {
	if *v > 0 {
		return
	}

	l.Log("config %v is %v; using the default %v", key, *v, def)
	*v = def
}

// removedKeys holds config keys that the module no longer reads.
type removedKeys struct {
	EntryLimit *uint64 `yaml:"entry_limit"`
}

// warnRemovedKeys logs each removed key that the config still sets.
//
// why: the config loader ignores unknown keys, so a node that still sets entry_limit would fall back to the default
// inode limit without a word.
func warnRemovedKeys(assets assets.Assets, log *log.Logger) {
	var keys removedKeys
	if err := assets.LoadYAML(vfsmod.ModuleName, &keys); err != nil {
		return
	}

	if keys.EntryLimit != nil {
		log.Log("config key entry_limit is ignored; use inode_limit")
	}
}
