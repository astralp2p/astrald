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

	transport := &nativeTransport{eval: e, ready: make(chan struct{})}
	binding, err := mod.coord.Register(mod.node.Identity(), names, transport)
	if err != nil {
		return nil, err
	}
	transport.binding = binding
	close(transport.ready)

	return &nativeRegistration{binding: binding}, nil
}

// nativeTransport answers each ask in-process from an Evaluator.
type nativeTransport struct {
	eval    servicesmod.Evaluator
	binding *coordinator.Binding
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

	return t.binding.Answer(&services.Answer{RequestID: ask.RequestID, Update: &u})
}

func (t *nativeTransport) Close() {}

type nativeRegistration struct {
	binding *coordinator.Binding
}

func (r *nativeRegistration) Changed() {
	_ = r.binding.Change(&services.Change{All: true})
}

func (r *nativeRegistration) Close() {
	r.binding.Close()
}
