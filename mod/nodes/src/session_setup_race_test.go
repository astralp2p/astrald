package nodes

import (
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// TestResetDuringSetupIsRaceFree: a Reset handled by the link's read loop while
// the query side installs the session's reader and writer reads them under the
// session lock, so `go test -race` reports no data race.
func TestResetDuringSetupIsRaceFree(t *testing.T) {
	for i := 0; i < 50; i++ {
		m := &Mux{}
		sess := newSession(0, astral.GenerateIdentity(), nil, "q", true)
		m.sessions.Set(sess.Nonce, sess)
		r, w := io.Pipe()

		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = sess.Setup(r, w)
		}()
		m.handleReset(&frames.Reset{Nonce: sess.Nonce})
		<-done
		sess.Close()
	}
}
