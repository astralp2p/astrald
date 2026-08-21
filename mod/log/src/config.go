package log

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

const DefaultLogLevel = 2

type Config struct {
	Level tree.Value[*astral.Uint8]
}
