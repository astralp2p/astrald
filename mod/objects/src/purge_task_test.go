package objects

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gopkg.in/yaml.v2"
)

// TestPurgeTask_FirstRunAfterOneInterval: the task purges local one interval after start, not at start.
func TestPurgeTask_FirstRunAfterOneInterval(t *testing.T) {
	mod := newTrackingModule(t)
	addLocalLeaf(t, mod, "disk1")
	id := commitData(t, astral.NewContext(nil), mod.GetRepository("disk1"), "old")
	setTimes(t, mod, "disk1", id, 1, 0)
	// why: mem0 is not in local, so the task leaves it alone.
	other := commitData(t, astral.NewContext(nil), mod.GetRepository("mem0"), "not local")
	setTimes(t, mod, "mem0", other, 1, 0)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	go mod.runPurgeTask(ctx, 500*time.Millisecond, 720*time.Hour)

	time.Sleep(100 * time.Millisecond)
	expectContains(t, mod, "disk1", id, true)

	deadline := time.Now().Add(5 * time.Second)
	for {
		has, _ := mod.GetRepository("disk1").Contains(astral.NewContext(nil), id)
		if !has {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task did not purge local")
		}
		time.Sleep(20 * time.Millisecond)
	}
	expectContains(t, mod, "mem0", other, true)
}

// TestPurgeTask_ZeroWindowIsOff: a window of 0 turns the task off.
func TestPurgeTask_ZeroWindowIsOff(t *testing.T) {
	mod := newTrackingModule(t)

	done := make(chan struct{})
	go func() {
		mod.runPurgeTask(astral.NewContext(nil), time.Millisecond, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the task runs with a window of 0")
	}
}

// TestConfig_PurgeUntouched: the window defaults to 720 hours, and the key purge_untouched sets it.
func TestConfig_PurgeUntouched(t *testing.T) {
	if got := defaultConfig.PurgeUntouched; got != 720*time.Hour {
		t.Fatalf("default purge_untouched %v; want 720h", got)
	}

	for text, want := range map[string]time.Duration{"purge_untouched: 0": 0, "purge_untouched: 48h": 48 * time.Hour} {
		cfg := defaultConfig
		if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
			t.Fatalf("unmarshal %q: %v", text, err)
		}
		if cfg.PurgeUntouched != want {
			t.Fatalf("%q gives %v; want %v", text, cfg.PurgeUntouched, want)
		}
	}
}
