package nodes

import (
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

type Frame struct {
	frames.Frame
	Source *Link
}
