package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestLaunchStampsMCPOrigin(t *testing.T) {
	q := launch(astral.NewQuery(astral.GenerateIdentity(), astral.GenerateIdentity(), "user.info"))

	if !q.IsMCP() {
		t.Fatal("launched query does not read as MCP origin")
	}
	if q.IsLocal() {
		t.Fatal("launched query still reads as local")
	}
}

// TestOnlyLaunchRoutes holds the invariant launch documents: the refusal lives
// in mod/shell and reads the origin key alone, so a query this module routes
// without going through launch reaches every node op.
func TestOnlyLaunchRoutes(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || name == "launch.go" {
			continue
		}

		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(src), "astral.Launch(") {
			t.Errorf("%v calls astral.Launch directly; route through launch() so the query carries its origin", name)
		}
	}
}
