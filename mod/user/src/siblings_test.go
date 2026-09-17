package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestRemoveSiblingCancelsOnce(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := astral.GenerateIdentity()
	var cancels int

	mod.addSibling(id, func() { cancels++ })
	mod.removeSibling(id)
	mod.removeSibling(id)

	if cancels != 1 {
		t.Fatalf("cancel called %d times, want 1", cancels)
	}
	if _, ok := mod.sibs.Get(id.String()); ok {
		t.Fatal("the sibling is still registered after removal")
	}
}

func TestRemoveUnknownSibling(t *testing.T) {
	mod := &Module{log: log.New(nil)}

	mod.removeSibling(astral.GenerateIdentity())
}
