package log

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	alog "github.com/astralp2p/astral-go/astral/log"
)

func TestLogEntryFilterPassesUpToTheLevel(t *testing.T) {
	for _, c := range []struct {
		name  string
		level *astral.Uint8
		pass  uint8
		drop  uint8
	}{
		{"unset uses the default", nil, DefaultLogLevel, DefaultLogLevel + 1},
		{"level 0", astral.NewUint8(0), 0, 1},
		{"level 5", astral.NewUint8(5), 5, 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod := &Module{}
			if c.level != nil {
				if err := mod.config.Level.Set(astral.NewContext(nil), c.level); err != nil {
					t.Fatalf("set level: %v", err)
				}
			}

			for lvl := uint8(0); lvl <= c.pass; lvl++ {
				if !mod.LogEntryFilter(alog.NewEntry(nil, lvl)) {
					t.Errorf("LogEntryFilter(level %d) = false, want true", lvl)
				}
			}
			if mod.LogEntryFilter(alog.NewEntry(nil, c.drop)) {
				t.Errorf("LogEntryFilter(level %d) = true, want false", c.drop)
			}
		})
	}
}
