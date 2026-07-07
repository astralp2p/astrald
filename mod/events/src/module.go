package events

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/resources"
)

type Deps struct {
	Objects objects.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
}

var _ events.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Emit(data astral.Object) *events.Event {
	e := &events.Event{
		ID:        astral.NewNonce(),
		SourceID:  mod.node.Identity(),
		Timestamp: astral.Now(),
		Data:      data,
	}
	go mod.Objects.Receive(e, mod.node.Identity())
	return e
}

func (mod *Module) String() string {
	return events.ModuleName
}
