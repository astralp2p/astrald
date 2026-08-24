package auth

import "time"

type Config struct {
	// ExternalAuthorizers defer named actions to an authority outside the node.
	// Empty — the default — leaves every decision to the registered handlers and
	// the contract chain, which is what the module did before.
	ExternalAuthorizers []ExternalConfig `yaml:"external_authorizers,omitempty"`
}

// ExternalConfig is one authority and the actions it decides.
type ExternalConfig struct {
	// Endpoint names the authority and how to reach it. The scheme selects the
	// transport:
	//
	//	http://127.0.0.1:8081/internal/authorize   the action is posted as JSON
	//	https://authz.example/authorize            the same, over TLS
	//	astral://telepathy:auth.authorize          the action is sent as a query
	//
	// why one field and not a transport switch beside an address: a scheme is
	// what a scheme is for, and two field groups with exactly one filled is a
	// shape yaml cannot state and a loader has to police.
	Endpoint string `yaml:"endpoint"`

	// Token authenticates the node to an http authority as a bearer token, and
	// TokenFile names a file holding one. TokenFile wins where both are set. An
	// astral authority needs neither: the query carries the node's identity.
	Token     string `yaml:"token,omitempty"`
	TokenFile string `yaml:"token_file,omitempty"`

	// Actions are the object types this authority decides. An action type names
	// one authority only: a second would make the decision an OR across
	// authorities, and an authority that can be outvoted is not one.
	Actions []string `yaml:"actions"`

	// Timeout bounds one question. An authorization sits inside whatever the
	// caller is holding open, so an answer that has not arrived within the bound
	// is one that arrives too late to use.
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

var defaultExternal = ExternalConfig{
	Timeout: 250 * time.Millisecond,
}

// withDefaults fills every bound the operator left unset. A zero is absent
// rather than chosen: a timeout of zero would refuse every question the moment
// it was asked.
func (c ExternalConfig) withDefaults() ExternalConfig {
	if c.Timeout <= 0 {
		c.Timeout = defaultExternal.Timeout
	}
	return c
}
