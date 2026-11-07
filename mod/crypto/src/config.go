package crypto

import (
	"time"

	"github.com/astralp2p/astral-go/api/objects"
)

const maxObjectSize = 4096

type Config struct {
	Repos []string

	// SignerTimeout bounds the wait for an app signer's answer. A zero or negative
	// value takes the default.
	SignerTimeout time.Duration `yaml:"signer_timeout,omitempty"`
}

var defaultConfig = Config{
	Repos: []string{objects.RepoLocal, objects.RepoSystem, "mem0"},

	// why 5m: a signer may wait for a person to approve on a device.
	SignerTimeout: 5 * time.Minute,
}
