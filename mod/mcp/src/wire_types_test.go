package mcp

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// The wire types are declared and registered by astral-go's api/mcp. A copy
// declared here as well would register the same object type twice, and
// astral.Add refuses the second — so one registration would lose and a decoder
// would materialize whichever type won. Assert astrald's build resolves both
// object types to the api package's, which holds only while this module
// declares neither.
func TestWireTypesResolveToAPI(t *testing.T) {
	for _, want := range []astral.Object{&mcp.Agent{}, &mcp.AgentInfo{}} {
		got := astral.New(want.ObjectType())
		if got == nil {
			t.Fatalf("%v is not registered", want.ObjectType())
		}
		if reflect.TypeOf(got) != reflect.TypeOf(want) {
			t.Fatalf("%v resolves to %T, want %T", want.ObjectType(), got, want)
		}
	}
}
