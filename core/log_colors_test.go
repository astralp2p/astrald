package core

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/resources"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestNewNodeDisableColorsSetsAsciiProfile: log.disable_colors in node.yaml
// turns colour off even when the output would otherwise be coloured.
func TestNewNodeDisableColorsSetsAsciiProfile(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	// note: stands in for a colour terminal on stdout.
	lipgloss.SetColorProfile(termenv.TrueColor)

	res := resources.NewMemResources()
	if err := res.Write("node.yaml", []byte("modules: []\nlog:\n  disable_colors: true\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewNode(astral.GenerateIdentity(), res); err != nil {
		t.Fatalf("NewNode: %v", err)
	}

	if got := lipgloss.ColorProfile(); got != termenv.Ascii {
		t.Fatalf("color profile = %v, want Ascii", got)
	}
}

// TestNewNodeKeepsColorProfileWhenColorsEnabled: without the flag the node
// leaves the detected profile alone.
func TestNewNodeKeepsColorProfileWhenColorsEnabled(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	lipgloss.SetColorProfile(termenv.TrueColor)

	res := resources.NewMemResources()
	if err := res.Write("node.yaml", []byte("modules: []\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewNode(astral.GenerateIdentity(), res); err != nil {
		t.Fatalf("NewNode: %v", err)
	}

	if got := lipgloss.ColorProfile(); got != termenv.TrueColor {
		t.Fatalf("color profile = %v, want TrueColor", got)
	}
}
