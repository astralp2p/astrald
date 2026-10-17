package nearby

import "github.com/astralp2p/astral-go/api/nearby"

const aliasPrefix = "."

type Config struct {
	Mode *nearby.Mode `yaml:"mode,omitempty"`
}

var defaultStealthMode = nearby.ModeStealth
var defaultConfig = Config{
	Mode: &defaultStealthMode,
}
