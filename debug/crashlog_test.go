package debug

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCrashLogTimeLayoutIs24Hour(t *testing.T) {
	morning := time.Date(2026, 9, 17, 2, 30, 0, 0, time.UTC).Format(crashLogTimeLayout)
	afternoon := time.Date(2026, 9, 17, 14, 30, 0, 0, time.UTC).Format(crashLogTimeLayout)

	if morning != "20260917023000" {
		t.Errorf("02:30 formatted as %q, want %q", morning, "20260917023000")
	}
	if afternoon != "20260917143000" {
		t.Errorf("14:30 formatted as %q, want %q", afternoon, "20260917143000")
	}
}

func TestSaveLogRepanicsWhenCrashFileCannotBeCreated(t *testing.T) {
	prev := LogDir
	t.Cleanup(func() { LogDir = prev })
	// note: the parent exists and the directory does not, so os.Create fails
	LogDir = filepath.Join(t.TempDir(), "missing")

	var afterCalls []any
	after := func(p any) { afterCalls = append(afterCalls, p) }

	repanic := func() (r any) {
		defer func() { r = recover() }()
		defer SaveLog(after)
		panic("boom")
	}()

	if repanic != "boom" {
		t.Errorf("re-panic value = %v, want %q", repanic, "boom")
	}
	if len(afterCalls) != 1 || afterCalls[0] != "boom" {
		t.Errorf("after calls = %v, want [boom]", afterCalls)
	}
}
