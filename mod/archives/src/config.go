package archives

import "github.com/astralp2p/astral-go/astral"

type Config struct {
	AutoIndexZones string
}

var defaultConfig = Config{
	AutoIndexZones: (astral.ZoneDevice | astral.ZoneVirtual).String(),
}
