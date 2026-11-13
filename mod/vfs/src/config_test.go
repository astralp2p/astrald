package vfs

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral/log"
	"gopkg.in/yaml.v2"
)

// The directory keys decode as the config loader decodes them, durations as Go durations.
func TestDirectoryConfigKeysDecodeStrictly(t *testing.T) {
	cfg := defaultConfig
	body := "directory_quiet_time: 200ms\ndirectory_longest_wait: 2s\ndirectory_bucket_size: 1024\n" +
		"directory_refill_rate: 10\ndirectory_min_charge: 512\ndirectory_node_rate: 100\n" +
		"directory_sweep_interval: 1m\ndirectory_grace_time: 2h\n"

	if err := yaml.UnmarshalStrict([]byte(body), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := Config{
		ByteLimit: defaultByteLimit, InodeLimit: defaultInodeLimit,
		DirectoryQuietTime: 200 * time.Millisecond, DirectoryLongestWait: 2 * time.Second,
		DirectoryBucketSize: 1024, DirectoryRefillRate: 10, DirectoryMinCharge: 512, DirectoryNodeRate: 100,
		DirectorySweepInterval: time.Minute, DirectoryGraceTime: 2 * time.Hour,
	}
	if cfg != want {
		t.Fatalf("decoded %+v, want %+v", cfg, want)
	}
}

// A value of 0 or below takes its default, and a minimum charge above the bucket takes the defaults of both.
func TestDirectoryConfigFallsBackToDefaults(t *testing.T) {
	cfg := defaultConfig
	cfg.DirectoryQuietTime, cfg.DirectoryNodeRate = -time.Second, 0
	cfg.DirectoryBucketSize, cfg.DirectoryMinCharge = 100, 200

	cfg.checkDirectoryValues(log.New(nil))

	if cfg != defaultConfig {
		t.Fatalf("checked %+v, want the defaults %+v", cfg, defaultConfig)
	}
}

// The directory defaults are the values of the ticket "VFS: store folder listings as objects".
func TestDirectoryConfigDefaults(t *testing.T) {
	want := Config{
		ByteLimit: defaultByteLimit, InodeLimit: defaultInodeLimit,
		DirectoryQuietTime: 5 * time.Second, DirectoryLongestWait: 60 * time.Second,
		DirectoryBucketSize: 8 << 20, DirectoryRefillRate: 4 << 10, DirectoryMinCharge: 4 << 10,
		DirectoryNodeRate: 32 << 10, DirectorySweepInterval: 10 * time.Minute, DirectoryGraceTime: time.Hour,
	}
	if defaultConfig != want {
		t.Fatalf("defaults %+v, want %+v", defaultConfig, want)
	}
}
