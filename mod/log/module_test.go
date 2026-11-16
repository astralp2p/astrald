package log

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// TestColorProfileFollowsStdout: importing the log module leaves the colour
// profile to detection, so output that is not a terminal stays plain.
func TestColorProfileFollowsStdout(t *testing.T) {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		t.Skip("stdout is a terminal")
	}
	if os.Getenv("CLICOLOR_FORCE") != "" {
		t.Skip("CLICOLOR_FORCE is set")
	}

	if got := lipgloss.ColorProfile(); got != termenv.Ascii {
		t.Fatalf("color profile = %v, want Ascii for non-terminal stdout", got)
	}
}
