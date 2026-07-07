package log

import (
	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
)

const DefaultLogLevel = 2

type Config struct {
	Level tree.Value[*astral.Uint8]
}
