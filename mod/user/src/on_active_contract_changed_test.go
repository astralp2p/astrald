package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

// entryChan is a log.EntryLogger that forwards every entry to a channel.
type entryChan chan *log.Entry

func (c entryChan) LogEntry(e *log.Entry) { c <- e }

// modeNearby is a nearby module that only answers SetMode. Every other method
// panics on the embedded nil interface.
type modeNearby struct {
	nearbymod.Module
}

func (modeNearby) SetMode(*astral.Context, nearby.Mode) error { return nil }

// TestOnActiveContractChangedLogsUnclaimed covers the unclaimed branch: a nil
// active contract logs exactly one line stating that the node has no user.
func TestOnActiveContractChangedLogsUnclaimed(t *testing.T) {
	const want = "this node runs without a user"

	entries := make(entryChan, 8)
	l := &log.Logger{}
	l.AddLogger(entries)

	mod := &Module{Deps: Deps{Nearby: modeNearby{}}, log: l}
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
