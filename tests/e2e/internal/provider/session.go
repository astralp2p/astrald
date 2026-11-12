// Package provider is the shared Go helper of the object-provider e2e drivers:
// the session loader, connect, app registration with ServeObjects permits,
// role registration, grant and revoke, and apps that serve objects.find,
// objects.describe, objects.search and objects.read with counting handlers in
// answer, claim or hang mode.
//
// A driver imports it as github.com/astralp2p/astrald/tests/e2e/internal/provider.
package provider

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// Node is one node of the session view (tests/lib/sessionio.py).
type Node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	Root     string `json:"root"`
}

// ID parses the node's identity.
func (n Node) ID() (*astral.Identity, error) { return astral.ParseIdentity(n.Identity) }

// Admin is a client of the node authenticated with the node's own token.
func (n Node) Admin() *astrald.Client { return Connect(n.Endpoint, n.Token) }

// Session is a driver's session view: the nodes and the facts of the tests
// before it.
type Session struct {
	Nodes map[string]Node            `json:"nodes"`
	Facts map[string]json.RawMessage `json:"facts"`
}

// Load reads a session view from path.
func Load(path string) (*Session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("session %s: %w", path, err)
	}
	return &s, nil
}

// Node returns the roster node name.
func (s *Session) Node(name string) (Node, error) {
	n, ok := s.Nodes[name]
	if !ok || n.Endpoint == "" {
		return Node{}, fmt.Errorf("the session holds no node %q", name)
	}
	return n, nil
}

// Fact decodes the fact name into v.
func (s *Session) Fact(name string, v any) error {
	raw, ok := s.Facts[name]
	if !ok {
		return fmt.Errorf("the session holds no %s fact", name)
	}
	return json.Unmarshal(raw, v)
}

// UserToken returns the user_token fact: node1's apphost token for the User.
func (s *Session) UserToken() (string, error) {
	var t string
	if err := s.Fact("user_token", &t); err != nil {
		return "", err
	}
	if t == "" {
		return "", fmt.Errorf("the session's user_token fact is empty")
	}
	return t, nil
}

// Connect returns a client of the apphost at endpoint authenticated with token.
func Connect(endpoint, token string) *astrald.Client {
	return astrald.New(apphost.NewRouter(endpoint, token))
}
