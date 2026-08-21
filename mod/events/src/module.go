package events

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/resources"
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
