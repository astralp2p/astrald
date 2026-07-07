package events

import "github.com/cryptopunkscc/astral-go/astral"

const ModuleName = "events"

type Module interface {
	Emit(data astral.Object) *Event
}
