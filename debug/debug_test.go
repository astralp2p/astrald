package debug

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
)

func setLogDir(t *testing.T, dir string) {
	t.Helper()
	prev := logDir
	if err := SetLogDir(dir); err != nil {
		t.Fatalf("SetLogDir(%q): %v", dir, err)
	}
	t.Cleanup(func() { logDir = prev })
}

func crashLogs(t *testing.T, dir string) []string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dir, "crash.*.log"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	return logs
}

func TestSaveLogWithoutPanic(t *testing.T) {
	dir := t.TempDir()
	setLogDir(t, dir)

	func() {
		defer SaveLog(nil)
	}()

	if logs := crashLogs(t, dir); len(logs) != 0 {
		t.Fatalf("crash logs = %q, want none", logs)
	}
}

func TestSaveLogWritesLogAndRepanics(t *testing.T) {
	dir := t.TempDir()
	setLogDir(t, dir)

	var afterArg any
	afterCalls := 0
	after := func(p any) {
		afterCalls++
		afterArg = p
	}

	recovered := func() (p any) {
		defer func() { p = recover() }()
		func() {
			defer SaveLog(after)
			panic("boom")
		}()
		return nil
	}()

	if recovered != "boom" {
		t.Fatalf("re-panicked value = %v, want %q", recovered, "boom")
	}
	if afterCalls != 1 || afterArg != "boom" {
		t.Fatalf("after called %d times with %v, want once with %q", afterCalls, afterArg, "boom")
	}

	logs := crashLogs(t, dir)
	if len(logs) != 1 {
		t.Fatalf("crash logs = %q, want exactly one", logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "panic: boom") {
		t.Fatalf("crash log content %q does not contain %q", data, "panic: boom")
	}
}

func crashAndRecover(msg string) {
	defer func() { recover() }()
	defer SaveLog(nil)
	panic(msg)
}

func TestSaveLogKeepsSameSecondCrashLogs(t *testing.T) {
	dir := t.TempDir()
	setLogDir(t, dir)

	crashAndRecover("first")
	crashAndRecover("second")

	logs := crashLogs(t, dir)
	if len(logs) != 2 {
		t.Fatalf("crash logs = %q, want two", logs)
	}
	var all string
	for _, log := range logs {
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		all += string(data)
	}
	for _, want := range []string{"panic: first", "panic: second"} {
		if !strings.Contains(all, want) {
			t.Errorf("crash logs do not contain %q", want)
		}
	}
}

func TestSetLogDirRefusesRelativePath(t *testing.T) {
	setLogDir(t, t.TempDir())
	prev := LogDir()

	if err := SetLogDir("rel"); !errors.Is(err, paths.ErrNotAbsolute) {
		t.Fatalf("SetLogDir(rel) error = %v, want %v", err, paths.ErrNotAbsolute)
	}
	if LogDir() != prev {
		t.Fatalf("LogDir() after a refused SetLogDir = %q, want %q", LogDir(), prev)
	}
}

// TestSaveLogWithoutDirWritesNoFile: a crash with no crash-log directory writes no file in the
// working directory and still re-panics.
func TestSaveLogWithoutDirWritesNoFile(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	setLogDir(t, "")

	crashAndRecover("boom")

	if logs := crashLogs(t, cwd); len(logs) != 0 {
		t.Fatalf("crash logs in the working directory = %q, want none", logs)
	}
}
