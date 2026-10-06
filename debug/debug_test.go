package debug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setLogDir(t *testing.T, dir string) {
	t.Helper()
	prev := LogDir
	LogDir = dir
	t.Cleanup(func() { LogDir = prev })
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
