package debug

import (
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
