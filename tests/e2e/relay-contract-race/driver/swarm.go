package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	nodescli "github.com/astralp2p/astral-go/api/nodes/client"
	usercli "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// node is one node of the session view.
type node struct {
	Endpoint    string `json:"endpoint"`
	Token       string `json:"token"`
	Identity    string `json:"identity"`
	HostIP      string `json:"host_ip"`
	LanEndpoint string `json:"lan_endpoint"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
	} `json:"facts"`
}

func loadSession(path string) (*session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	for _, name := range names {
		if s.Nodes[name].Endpoint == "" {
			return nil, fmt.Errorf("the session view holds no %s", name)
		}
	}
	return &s, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func admin(n node) *astrald.Client { return connect(n.Endpoint, n.Token) }

// joinSwarm adopts node3 and node4 as the User on node1, each after node1 and
// the new node learn each other's endpoint. The adoption pushes tell node2 and
// the other new node the rest.
func joinSwarm(ctx *astral.Context, s *session) error {
	n1 := s.Nodes["node1"]
	for _, name := range []string{"node3", "node4"} {
		n := s.Nodes[name]
		if err := addEndpoint(ctx, admin(n1), n); err != nil {
			return err
		}
		if err := addEndpoint(ctx, admin(n), n1); err != nil {
			return err
		}
		actx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(60 * time.Second)
		_, err := usercli.New(nil, connect(n1.Endpoint, s.Facts.UserToken)).Adopt(actx, n.Identity)
		cancel()
		if err != nil {
			return fmt.Errorf("adopting %s on node1: %w", name, err)
		}
	}
	return nil
}

func addEndpoint(ctx *astral.Context, c *astrald.Client, peer node) error {
	ch, err := c.QueryChannel(ctx, "nodes.add_endpoint", query.Args{"identity": peer.Identity, "endpoint": peer.LanEndpoint})
	if err != nil {
		return fmt.Errorf("nodes.add_endpoint %v: %w", peer.Identity, err)
	}
	defer ch.Close()
	if err := ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return fmt.Errorf("nodes.add_endpoint %v: %w", peer.Identity, err)
	}
	return nil
}

// awaitMesh waits until every node holds a link to each of the other three.
func awaitMesh(ctx *astral.Context, s *session) error {
	deadline := time.Now().Add(converge)
	for {
		missing, err := unlinked(ctx, s)
		if err == nil && len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("after %v, links missing: %v (err %v)", converge, missing, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// unlinked lists every "from->to" for which node from holds no link to node to.
func unlinked(ctx *astral.Context, s *session) ([]string, error) {
	var missing []string
	for _, from := range names {
		links, err := nodescli.New(nil, admin(s.Nodes[from])).Links(ctx)
		if err != nil {
			return nil, fmt.Errorf("nodes.links on %s: %w", from, err)
		}
		var peers []string
		for _, l := range links {
			peers = append(peers, l.RemoteIdentity.String())
		}
		for _, to := range names {
			if to != from && !slices.Contains(peers, s.Nodes[to].Identity) {
				missing = append(missing, from+"->"+to)
			}
		}
	}
	return missing, nil
}
