package archives

import "github.com/cryptopunkscc/astral-go/astral"

type Config struct {
	AutoIndexZones string
}

var defaultConfig = Config{
	AutoIndexZones: (astral.ZoneDevice | astral.ZoneVirtual).String(),
}
