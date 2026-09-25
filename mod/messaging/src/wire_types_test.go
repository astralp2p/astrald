package messaging

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// The wire types are declared and registered by astral-go's api/messaging. A
// copy declared here as well would register the same object type twice, and
// astral.Add refuses the second — so one registration would lose and a decoder
// would materialize whichever type won. Assert astrald's build resolves every
// object type this module sends or reads to the api package's, which holds only
// while this module declares none of them.
func TestWireTypesResolveToAPI(t *testing.T) {
	for _, want := range []astral.Object{
		&messaging.IdentityCredential{}, &messaging.IdentityInfo{},
		&messaging.Message{}, &messaging.Receipt{},
		&messaging.Envelope{}, &messaging.MessageID{},
		&messaging.SendMessageRequest{}, &messaging.ReadMessagesRequest{},
		&messaging.ReadMessagesResult{}, &messaging.WaitResult{},
		&messaging.ArchiveResult{},
	} {
		got := astral.New(want.ObjectType())
		if got == nil {
			t.Fatalf("%v is not registered", want.ObjectType())
		}
		if reflect.TypeOf(got) != reflect.TypeOf(want) {
			t.Fatalf("%v resolves to %T, want %T", want.ObjectType(), got, want)
		}
	}
}
