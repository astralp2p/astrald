package core

import (
	"bytes"
	"errors"
	"io"
	_log "log"

	"github.com/astralp2p/astral-go/astral/log"
)

const maxAdapterBuffer = 1 << 16 // 64kb

type adapter struct {
	*log.Logger
	b []byte
}

var _ io.Writer = &adapter{}

func (node *Node) initLogger() {
	node.log = log.New(node.identity)

	_log.SetOutput(adapter{Logger: node.log.Tag("stdout")})
	_log.SetFlags(0)
}

func (a adapter) Write(p []byte) (n int, err error) {
	if len(a.b)+len(p) > maxAdapterBuffer {
		return 0, errors.New("log buffer overflow")
	}

	a.b = append(a.b, p...)
	n = len(p)

	idx := bytes.IndexByte(a.b, '\n')
	if idx == -1 {
		return
	}

	line := a.b[:idx]
	a.b = a.b[idx+1:]

	a.Logger.Log("%v", string(line))

	return
}
