package core

import (
	"bytes"
	"errors"
	"io"
	_log "log"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const maxAdapterBuffer = 1 << 16 // 64kb

type adapter struct {
	*log.Logger
	b []byte
}

var _ io.Writer = &adapter{}

func (node *Node) initLogger() {
	node.log = log.New(node.identity)

	_log.SetOutput(&adapter{Logger: node.log.Tag("stdout")})
	_log.SetFlags(0)
}

// applyLogColors turns log colours off when node.yaml sets log.disable_colors.
// note: otherwise lipgloss detects the profile from stdout, so a file, pipe or
// journal gets plain text and NO_COLOR is honoured.
func (node *Node) applyLogColors() {
	if node.config.Log.DisableColors {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

func (a *adapter) Write(p []byte) (n int, err error) {
	if len(a.b)+len(p) > maxAdapterBuffer {
		return 0, errors.New("log buffer overflow")
	}

	a.b = append(a.b, p...)
	n = len(p)

	// why: one Write can carry several lines, and can end mid-line — the
	// remainder stays in a.b until a later Write completes it.
	for {
		idx := bytes.IndexByte(a.b, '\n')
		if idx == -1 {
			return
		}

		line := string(a.b[:idx])
		a.b = a.b[idx+1:]

		a.Logger.Log("%v", line)
	}
}
