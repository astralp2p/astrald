package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// entryChan is a log.EntryLogger that forwards every entry to a channel.
type entryChan chan *log.Entry

func (c entryChan) LogEntry(e *log.Entry) { c <- e }

// TestOnActiveContractChangedLogsUnclaimed covers the unclaimed branch: a nil
// active contract logs exactly one line stating that the node has no user.
func TestOnActiveContractChangedLogsUnclaimed(t *testing.T) {
	const want = "this node runs without a user"

	entries := make(entryChan, 8)
	l := &log.Logger{}
	l.AddLogger(entries)

	mod := &Module{log: l}
	mod.onActiveContractChanged(nil)

	if len(entries) != 1 {
		t.Fatalf("logged %v entries; want 1", len(entries))
	}

	var line string
	for _, obj := range (<-entries).Objects {
		line += astral.Stringify(obj)
	}
	if line != want {
		t.Errorf("logged %q; want %q", line, want)
	}
}
