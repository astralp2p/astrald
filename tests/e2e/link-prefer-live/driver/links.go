package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	nodescli "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// relayOnly links node2 to node1 through relay, closes every other link
// between them, and returns the relayed link's id once both nodes hold it alone.
func relayOnly(ctx *astral.Context, p pair, relay *relay) (astral.Nonce, error) {
	info, err := newLink(ctx, p.admin2, p.n1.Identity, "tcp:"+relay.Addr())
	if err != nil {
		return 0, fmt.Errorf("linking node2 to node1 through the relay: %w", err)
	}
	relayed := info.ID
	ids, err := linkIDs(ctx, p.admin1, p.n2.Identity)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if id == relayed {
			continue
		}
		if err := closeLink(ctx, p.admin1, id); err != nil {
			return 0, err
		}
	}
	alone := func(ids []astral.Nonce) bool { return len(ids) == 1 && ids[0] == relayed }
	if !waitLinks(ctx, p.admin1, p.n2.Identity, alone) || !waitLinks(ctx, p.admin2, p.n1.Identity, alone) {
		return 0, fmt.Errorf("node1 and node2 did not settle on the relayed link %v alone", relayed)
	}
	return relayed, nil
}

// linkBoth links node2 to node1 at endpoint and returns the new link's id
// once both nodes list it.
func linkBoth(ctx *astral.Context, p pair, endpoint string) (astral.Nonce, error) {
	info, err := newLink(ctx, p.admin2, p.n1.Identity, endpoint)
	if err != nil {
		return 0, err
	}
	has := func(ids []astral.Nonce) bool { return slices.Contains(ids, info.ID) }
	if !waitLinks(ctx, p.admin1, p.n2.Identity, has) || !waitLinks(ctx, p.admin2, p.n1.Identity, has) {
		return 0, fmt.Errorf("node1 and node2 do not both list the link %v", info.ID)
	}
	return info.ID, nil
}

// newLink asks the admin's node to link to identity at endpoint.
func newLink(ctx *astral.Context, admin *astrald.Client, identity, endpoint string) (*nodes.LinkInfo, error) {
	lctx, cancel := ctx.WithTimeout(converge)
	defer cancel()
	ch, err := admin.QueryChannel(lctx, "nodes.new_link", query.Args{"identity": identity, "endpoint": endpoint})
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	var info *nodes.LinkInfo
	if err := ch.Switch(channel.Expect(&info), channel.PassErrors, channel.WithContext(lctx), channel.FailOnEOF()); err != nil {
		return nil, err
	}
	return info, nil
}

func closeLink(ctx *astral.Context, admin *astrald.Client, id astral.Nonce) error {
	ch, err := admin.QueryChannel(ctx, "nodes.close_link", query.Args{"link_id": id.String()})
	if err != nil {
		return fmt.Errorf("nodes.close_link %v: %w", id, err)
	}
	defer ch.Close()
	if err := ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return fmt.Errorf("nodes.close_link %v: %w", id, err)
	}
	return nil
}

// linkIDs returns the ids of the admin's node's links to peer.
func linkIDs(ctx *astral.Context, admin *astrald.Client, peer string) ([]astral.Nonce, error) {
	links, err := nodescli.New(nil, admin).Links(ctx)
	if err != nil {
		return nil, fmt.Errorf("nodes.links: %w", err)
	}
	var ids []astral.Nonce
	for _, l := range links {
		if l.RemoteIdentity.String() == peer {
			ids = append(ids, l.ID)
		}
	}
	return ids, nil
}

// waitLinks polls the admin's node's links to peer until ok holds for their ids.
func waitLinks(ctx *astral.Context, admin *astrald.Client, peer string, ok func([]astral.Nonce) bool) bool {
	deadline := time.Now().Add(converge)
	for time.Now().Before(deadline) {
		if ids, err := linkIDs(ctx, admin, peer); err == nil && ok(ids) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
