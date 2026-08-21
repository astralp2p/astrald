package events

import "github.com/astralp2p/astral-go/astral"

const ModuleName = "events"

type Module interface {
	Emit(data astral.Object) *Event
}
