package nearby

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/nearby"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

const aliasPrefix = "."

type Config struct {
	Mode *nearby.Mode `yaml:"mode,omitempty"`
	// SilentListen decides whether a Silent node emits PeerSeen; unset reads true.
	SilentListen *bool `yaml:"silent_listen,omitempty"`
}

// validate refuses a mode above ModeStealth.
func (c Config) validate() error {
	if c.Mode != nil && *c.Mode > nearby.ModeStealth {
		return fmt.Errorf("mode %v: %w", *c.Mode, nearbymod.ErrInvalidMode)
	}
	return nil
}
