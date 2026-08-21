package apphost

import (
	"errors"
	"io"
	"strings"

	"github.com/astralp2p/astral-go/astral"
)

func (mod *Module) worker(ctx *astral.Context) error {
	for conn := range mod.conns {
		var done = make(chan struct{})

		go func() {
			select {
			case <-ctx.Done():
				conn.Close()
			case <-done:
			}
		}()

		err := NewGuest(mod, conn).Serve(ctx)

		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
		case strings.Contains(err.Error(), "connection closed"):
		case strings.Contains(err.Error(), "use of closed network connection"):
		case strings.Contains(err.Error(), "read/write on closed pipe"):
		default:
			mod.log.Error("serve error: %v", err)
		}

		conn.Close()
		close(done)
	}
	return nil
}
