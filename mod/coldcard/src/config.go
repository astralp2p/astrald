package coldcard

import "time"

type Config struct {
	// ScanInterval is how often the module enumerates the attached ColdCards. A
	// device plugged in after start is recorded on the next scan.
	ScanInterval time.Duration `yaml:"scan_interval,omitempty"`
}

var defaultConfig = Config{
	// why 10s: a plugged-in device becomes a signer within that window.
	// note: one scan is one short-lived ckcc process.
	ScanInterval: 10 * time.Second,
}
