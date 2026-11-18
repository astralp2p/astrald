package nearby

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	ethermod "github.com/astralp2p/astrald/mod/ether"
)

// entryRecorder records the text of every level-0 log entry.
type entryRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *entryRecorder) LogEntry(e *log.Entry) {
	if e.Level != 0 {
		return
	}
	var b strings.Builder
	for _, o := range e.Objects {
		b.WriteString(astral.Stringify(o))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, b.String())
}

// sendErrors returns the recorded level-0 lines of a failed status or scan send.
func (r *entryRecorder) sendErrors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, l := range r.lines {
		if strings.HasPrefix(l, "broadcast status:") || strings.HasPrefix(l, "scan:") {
			out = append(out, l)
		}
	}
	return out
}

// With LAN discovery off, the periodic status and the address-change status and scan log
// no level-0 error; any other send error still logs at level 0.
func TestSendErrorLogWhileLANDiscoveryIsOff(t *testing.T) {
	// note: the error shape ether's broadcast returns without a socket.
	lanOffErr := errors.Join(fmt.Errorf("send to 192.168.1.255: %w", ethermod.ErrNotListening))

	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{"lan discovery off", lanOffErr, 0},
		{"other send error", errors.New("network is unreachable"), 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			owner := astral.GenerateIdentity()
			h := newHarness(t, owner)
			h.setMode(t, nearby.ModeVisible)
			h.mod.AddStatusComposer(&modeComposer{mod: h.mod, owner: owner})
			h.ether.err = tt.err
			rec := &entryRecorder{}
			h.mod.log.AddLogger(rec)

			ctx, cancel := astral.NewContext(nil).WithCancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.mod.runPeriodic(ctx)
			}()
			h.ether.waitSend(t)
			h.clock.nextWait(t) // note: the loop logged, or not, before it waits
			cancel()
			<-done

			h.mod.receiveNetworkAddressChanged(&ip.EventNetworkAddressChanged{Added: []ip.IP{ipOf("192.168.1.30")}})
			if pushed, _ := h.ether.recorded(); len(pushed) != 3 {
				t.Fatalf("sent %d datagrams; want two statuses and a scan", len(pushed))
			}

			if got := rec.sendErrors(); len(got) != tt.want {
				t.Errorf("logged %d send errors at level 0 %q; want %d", len(got), got, tt.want)
			}
		})
	}
}
