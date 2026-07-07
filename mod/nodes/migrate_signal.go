package nodes

import (
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// MigrateSignal is exchanged over the signalling channel during session migration.
// Buffer carries the sender's receive-buffer size so the peer can set its write window.
type MigrateSignal struct {
	Signal astral.String8
	Buffer astral.Uint32
}

const (
	MigrateSignalReady    astral.String8 = "ready"
	MigrateSignalSwitched astral.String8 = "switched"
	MigrateSignalResume   astral.String8 = "resume"
	MigrateSignalDone     astral.String8 = "done"
)

func (MigrateSignal) ObjectType() string { return "mod.nodes.migrate_signal" }

func (m MigrateSignal) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&m).WriteTo(w)
}

func (m *MigrateSignal) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(m).ReadFrom(r)
}

func init() { _ = astral.Add(&MigrateSignal{}) }

// ExpectMigrateSignal returns a channel.Switch handler that accepts the given signal type.
// If buf is non-nil, the peer's buffer size is written to it.
func ExpectMigrateSignal(signalType astral.String8, buf *astral.Uint32) func(*MigrateSignal) error {
	return func(sig *MigrateSignal) error {
		if sig.Signal != signalType {
			return fmt.Errorf("expected %s signal, got %s", signalType, sig.Signal)
		}
		if buf != nil {
			*buf = sig.Buffer
		}
		return channel.ErrBreak
	}
}
