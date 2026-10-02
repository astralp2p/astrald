package services

import (
	"github.com/astralp2p/astral-go/api/services"
	servicesmod "github.com/astralp2p/astrald/mod/services"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// RegisterNative makes the node identity the provider of names, answered by e.
func (mod *Module) RegisterNative(names []string, e servicesmod.Evaluator) (servicesmod.Registration, error) {
	for _, name := range names {
		if err := services.ValidateName(name); err != nil {
			return nil, err
		}
	}

	t := &nativeTransport{eval: e, ready: make(chan struct{})}
	src, err := mod.coord.Register(mod.node.Identity(), names, t)
	if err != nil {
		return nil, err
	}
	t.src = src
	close(t.ready)

	return &nativeRegistration{src: src}, nil
}

// nativeTransport answers each ask in-process from an Evaluator.
type nativeTransport struct {
	eval servicesmod.Evaluator
	src  *coordinator.Source
	// why: Register can queue asks for existing followers before it returns
	// the source; ready holds them until src is set.
	ready chan struct{}
}

func (t *nativeTransport) Send(ask *services.Ask) error {
	<-t.ready

	var u services.Update
	if v := t.eval.Evaluate(ask.CallerID, string(ask.Service)); v != nil {
		u = *v
	}
	u.Name = ask.Service
	u.ProviderID = nil

	return t.src.Answer(&services.Answer{RequestID: ask.RequestID, Update: &u})
}

func (t *nativeTransport) Close() {}

type nativeRegistration struct {
	src *coordinator.Source
}

func (r *nativeRegistration) Changed() {
	_ = r.src.Change(&services.Change{All: true})
}

func (r *nativeRegistration) Close() {
	r.src.Close()
}
