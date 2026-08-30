package mcp

import (
	"testing"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// seedID makes distinct ids in bulk, past the one-byte range testID covers.
func seedID(n int) (id mcpapi.MessageID) {
	id[0], id[1] = byte(n), byte(n>>8)
	return
}

// How much of a listing the module hands out in one answer is the module's
// rule and not the caller's: a caller naming no limit gets the default, and one
// naming a larger number than the cap gets the cap.
func TestListLimitsAreTheModulesRuleAndNotTheCallers(t *testing.T) {
	for _, c := range []struct {
		name string
		seed func(mod *Module, id mcpapi.MessageID, agent *astral.Identity) error
		list func(mod *Module, agent *astral.Identity, limit int) (int, error)
		def  int
		max  int
	}{
		{
			name: "inbox",
			seed: func(mod *Module, id mcpapi.MessageID, agent *astral.Identity) error {
				return mod.db.InsertMessage(&dbMessage{
					ID: id, Sender: astral.GenerateIdentity(), Recipient: agent, Content: "x",
				})
			},
			list: func(mod *Module, agent *astral.Identity, limit int) (int, error) {
				rows, err := mod.listInbox(agent, false, limit)
				return len(rows), err
			},
			def: defaultInboxLimit,
			max: maxInboxLimit,
		},
		{
			name: "outbox",
			seed: func(mod *Module, id mcpapi.MessageID, agent *astral.Identity) error {
				return mod.db.InsertOutbox(&dbOutbox{
					ID: id, Sender: agent, Recipient: astral.GenerateIdentity(), Content: "x",
				})
			},
			list: func(mod *Module, agent *astral.Identity, limit int) (int, error) {
				rows, err := mod.listOutbox(agent, limit)
				return len(rows), err
			},
			def: defaultOutboxLimit,
			max: maxOutboxLimit,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod := testMessageModule(t)
			agent := astral.GenerateIdentity()

			for i := range c.max + 1 {
				if err := c.seed(mod, seedID(i), agent); err != nil {
					t.Fatalf("seed %v: %v", i, err)
				}
			}

			for _, want := range []struct {
				asked int
				got   int
				why   string
			}{
				{0, c.def, "no limit named takes the default"},
				{-1, c.def, "a nonsense limit takes the default"},
				{5, 5, "a limit under the cap is honoured"},
				{c.max + 500, c.max, "a limit over the cap is capped"},
			} {
				n, err := c.list(mod, agent, want.asked)
				if err != nil {
					t.Fatalf("list(%v): %v", want.asked, err)
				}
				if n != want.got {
					t.Fatalf("list(%v) returned %v, want %v — %v", want.asked, n, want.got, want.why)
				}
			}
		})
	}
}
