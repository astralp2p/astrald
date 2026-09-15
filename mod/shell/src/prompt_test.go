package shell

import (
	"bytes"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/shell"
)

func TestPromptResolvesByTypeName(t *testing.T) {
	obj := astral.New("mod.shell.prompt")
	if _, ok := obj.(*Prompt); !ok {
		t.Fatalf("astral.New(%q) returned %T, want *Prompt", "mod.shell.prompt", obj)
	}
}

func TestTerminalPrintWritesPromptToStream(t *testing.T) {
	var buf bytes.Buffer
	p := &Prompt{guestID: astral.GenerateIdentity(), hostID: astral.GenerateIdentity()}

	if err := shell.NewTerminal(&buf).Print(p); err != nil {
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
