// Package policyconf reads the policy values of a module's `policies` config
// block.
package policyconf

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
)

// The modes a Setting takes as a scalar.
const (
	AcceptAll = "accept_all"
	RejectAll = "reject_all"
)

// Setting is one policy value: the scalar accept_all or reject_all, or a
// delegate mapping. The zero Setting selects RejectAll.
type Setting struct {
	Mode     string
	Delegate *DelegateSetting
}

// DelegateSetting names the op that decides, as policy.Delegate does. Identity
// is resolved when a question is asked.
type DelegateSetting struct {
	Identity  string        `yaml:"identity"`
	Operation string        `yaml:"operation"`
	Timeout   time.Duration `yaml:"timeout"`
}

// UnmarshalYAML reads a scalar mode or a delegate mapping. Any other scalar,
// and a mapping without identity or operation, is an error.
func (s *Setting) UnmarshalYAML(unmarshal func(any) error) error {
	var mode string
	if err := unmarshal(&mode); err == nil {
		switch mode {
		case AcceptAll, RejectAll:
			*s = Setting{Mode: mode}
			return nil
		}
		return fmt.Errorf("policy %q: want %v, %v or a delegate mapping", mode, AcceptAll, RejectAll)
	}

	var d DelegateSetting
	if err := unmarshal(&d); err != nil {
		return err
	}
	if d.Identity == "" || d.Operation == "" {
		return errors.New("policy delegate: identity and operation are required")
	}
	if d.Timeout < 0 || d.Timeout > policy.MaxTimeout {
		return fmt.Errorf("policy delegate: timeout %v is outside 0..%v", d.Timeout, policy.MaxTimeout)
	}
	*s = Setting{Delegate: &d}
	return nil
}

// Accepts reports whether s selects AcceptAll.
func (s Setting) Accepts() bool {
	return s.Delegate == nil && s.Mode == AcceptAll
}

// ResolveDelegate returns the delegate s names, resolving its identity with
// resolve, or nil when s names a mode.
func (s Setting) ResolveDelegate(resolve func(string) (*astral.Identity, error)) (*policy.Delegate, error) {
	if s.Delegate == nil {
		return nil, nil
	}
	id, err := resolve(s.Delegate.Identity)
	if err != nil {
		return nil, err
	}
	d := &policy.Delegate{
		Identity:  id,
		Operation: astral.String8(s.Delegate.Operation),
		Timeout:   astral.Duration(s.Delegate.Timeout),
	}
	return d, d.Validate()
}
