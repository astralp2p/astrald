package mcp

import (
	"encoding/json"
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// AgentInfo is an agent's record without its access token: what a caller may
// read about an agent it does not hold the credentials for.
//
// why a second type and not Agent with the token left empty: a caller cannot
// tell "this agent has no token" from "the token was withheld", and a type that
// sometimes carries a secret is one refactor away from carrying it always.
type AgentInfo struct {
	Identity  *astral.Identity
	Alias     astral.String8
	Exposed   astral.Bool
	ExpiresAt astral.Time
}

// astral

var _ astral.Object = &AgentInfo{}

func (a AgentInfo) ObjectType() string { return "mcp.agent_info" }

func (a AgentInfo) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *AgentInfo) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

// json

func (a AgentInfo) MarshalJSON() ([]byte, error) {
	type alias AgentInfo
	return json.Marshal(alias(a))
}

func (a *AgentInfo) UnmarshalJSON(bytes []byte) error {
	type alias AgentInfo
	var v alias

	err := json.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}

	*a = AgentInfo(v)
	return nil
}

func init() {
	_ = astral.Add(&AgentInfo{})
}
