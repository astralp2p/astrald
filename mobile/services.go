package mobile

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	servicesmod "github.com/astralp2p/astrald/mod/services"
)

// ServiceEvaluator decides what a node-native service offers a caller. The
// platform wrapper implements it; it runs on the node's evaluation goroutine and
// must return quickly.
type ServiceEvaluator interface {
	// Available reports whether service is offered to caller, an identity in
	// hex.
	Available(caller, service string) bool
	// Operations lists the operations the offering exposes, comma-separated, or
	// "" for none.
	Operations(service string) string
}

// ServiceRegistration is a node-native service the platform wrapper holds.
type ServiceRegistration struct {
	reg servicesmod.Registration
}

// Changed tells the node that every caller's offering may have changed.
func (r *ServiceRegistration) Changed() { r.reg.Changed() }

// Close withdraws the services; consumers that saw them receive a removal.
func (r *ServiceRegistration) Close() { r.reg.Close() }

// RegisterService advertises names, comma-separated, as services of this node,
// answered by e. Callers discover them like any other service; the provider is
// the node identity. The node must be running and its modules loaded; until
// then it returns an error and the wrapper retries.
func (n *Node) RegisterService(names string, e ServiceEvaluator) (*ServiceRegistration, error) {
	n.mu.Lock()
	cnode := n.cnode
	n.mu.Unlock()

	if cnode == nil || !n.running.Load() {
		return nil, errors.New("node not running")
	}

	mod, err := core.Load[servicesmod.Module](cnode, servicesmod.ModuleName)
	if err != nil {
		return nil, err
	}
	return registerService(mod, names, e)
}

func registerService(mod servicesmod.Module, names string, e ServiceEvaluator) (*ServiceRegistration, error) {
	if e == nil {
		return nil, errors.New("no evaluator")
	}
	list, err := services.ParseNames(names)
	if err != nil {
		return nil, err
	}
	reg, err := mod.RegisterNative(list, nativeEvaluator{e})
	if err != nil {
		return nil, err
	}
	return &ServiceRegistration{reg: reg}, nil
}

// nativeEvaluator adapts the platform's evaluator to the services module.
type nativeEvaluator struct {
	e ServiceEvaluator
}

func (a nativeEvaluator) Evaluate(caller *astral.Identity, service string) *services.Update {
	if !a.e.Available(caller.String(), service) {
		return nil
	}
	u := &services.Update{Available: true}
	if ops := a.e.Operations(service); ops != "" {
		list := &services.OperationsList{}
		for _, op := range strings.Split(ops, ",") {
			list.Operations = append(list.Operations, astral.String8(strings.TrimSpace(op)))
		}
		u.Info = astral.NewBundle()
		_ = u.Info.Append(list)
	}
	return u
}
