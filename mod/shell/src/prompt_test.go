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
	p := &Prompt{GuestID: astral.GenerateIdentity(), HostID: astral.GenerateIdentity()}

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

func TestPromptRoundTripsBothIdentities(t *testing.T) {
	var buf bytes.Buffer
	sent := &Prompt{GuestID: astral.GenerateIdentity(), HostID: astral.GenerateIdentity()}

	if _, err := sent.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("WriteTo wrote no bytes, want the encoded identities")
	}

	var got Prompt
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatal(err)
	}

	if !got.GuestID.IsEqual(sent.GuestID) {
		t.Errorf("GuestID = %v, want %v", got.GuestID, sent.GuestID)
	}
	if !got.HostID.IsEqual(sent.HostID) {
		t.Errorf("HostID = %v, want %v", got.HostID, sent.HostID)
	}
}

// why: the zero value of a registered type is reachable over the wire, so the
// codec decodes a Prompt whose identities were never set.
func TestPromptRoundTripsZeroValue(t *testing.T) {
	var buf bytes.Buffer

	if _, err := (&Prompt{}).WriteTo(&buf); err != nil {
		t.Fatal(err)
	}

	var got Prompt
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatal(err)
	}
}
