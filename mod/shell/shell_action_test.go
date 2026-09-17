package shell

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func TestShellActionRoundTrips(t *testing.T) {
	sent := ShellAction{Action: auth.NewAction(astral.GenerateIdentity())}

	var buf bytes.Buffer
	written, err := sent.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got ShellAction
	read, err := got.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if read != written {
		t.Fatalf("ReadFrom read %d bytes, WriteTo wrote %d", read, written)
	}
	if got.Nonce != sent.Nonce {
		t.Errorf("Nonce = %v, want %v", got.Nonce, sent.Nonce)
	}
	if !got.ActorID.IsEqual(sent.ActorID) {
		t.Errorf("ActorID = %v, want %v", got.ActorID, sent.ActorID)
	}
}

func TestShellActionResolvesByTypeName(t *testing.T) {
	const name = "mod.shell.shell_action"
	obj := astral.New(name)
	if _, ok := obj.(*ShellAction); !ok {
		t.Fatalf("astral.New(%q) returned %T, want *ShellAction", name, obj)
	}
}
