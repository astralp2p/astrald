package log

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/tree"
)

const DefaultLogLevel = 2

type Config struct {
	Level tree.Value[*astral.Uint8]
}
