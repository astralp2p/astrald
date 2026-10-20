package shell

import (
	"bytes"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/shell"
	"github.com/astralp2p/astral-go/astral"
	shellmod "github.com/astralp2p/astrald/mod/shell"
)

func TestTerminalPrintWritesPromptToStream(t *testing.T) {
	var buf bytes.Buffer
	p := &shell.Prompt{GuestID: astral.GenerateIdentity(), HostID: astral.GenerateIdentity()}

	if err := shellmod.NewTerminal(&buf).Print(p); err != nil {
		t.Fatal(err)
	}

	want := p.Render()
	if !strings.HasSuffix(want, "> ") {
		t.Fatalf("Render() = %q, want a prompt ending in %q", want, "> ")
	}
	if got := buf.String(); got != want {
		t.Fatalf("stream holds %q, want %q", got, want)
	}
}
