package services

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "services"

// Module hosts the node's service providers and answers discovery from them.
type Module interface {
	// RegisterNative makes the node itself the provider of names. It claims
	// every name or none, like an app advertisement, and is called in Prepare.
	RegisterNative(names []string, e Evaluator) (Registration, error)
}

// Evaluator decides what a native service offers one caller.
type Evaluator interface {
	// Evaluate returns the caller's offering of service, or nil for none. It
	// runs outside the module's lock, must not block, and must not call back
	// into the module.
	Evaluate(caller *astral.Identity, service string) *services.Update
}

// Registration is one native provider's hold on its names.
type Registration interface {
	// Changed tells the module that every caller's offering may have changed.
	Changed()
	// Close releases the names. Consumers that saw them receive a removal.
	Close()
}
