package user

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// The SwarmState exchange between two nodes, over queries to the user module, served by OpSwarmDigest,
// OpSwarmFetch and OpSwarmPush:
//
//	swarm_digest  answers the ID of every stored entry, then EOS.
//	swarm_fetch   receives entry IDs until EOS, answers each stored entry among them, then EOS.
//	swarm_push    receives one entry and answers Ack when it is stored or was already, or the error.
//
// A node pulls what it lacks (digest, then fetch of the missing IDs) and pushes each entry it stores to its peers.
// Every entry goes through the store's merge on the receiving node, so a merge from a peer is checked like a local write.
//
// Spec: the ops are named user.swarm_digest, user.swarm_fetch and user.swarm_push. They were nodes.swarm_*
// while mod/nodes held SwarmState. The rename is one cutover with no alias for the old names (D11, new swarms only).
const (
	opSwarmDigest = "swarm_digest"
	opSwarmFetch  = "swarm_fetch"
	opSwarmPush   = "swarm_push"

	// swarmFetchBatch is the most IDs one swarm_fetch asks for.
	swarmFetchBatch = 256
)

// pullSwarm fetches the entries src stores and this node lacks, and adds each one. It returns how much the head
// grew. Entries that wait are held; entries that are dropped are not stored.
func (s *swarmStore) pullSwarm(ctx *astral.Context, src *routerSwarmPeer) (uint64, error) {
	before := s.set.Head()

	digest, err := src.SwarmDigest(ctx)
	if err != nil {
		return 0, fmt.Errorf("swarm digest: %w", err)
	}

	var missing []*astral.ObjectID
	for _, id := range digest {
		if id != nil && !s.set.Has(id) {
			missing = append(missing, id)
		}
	}

	for len(missing) > 0 {
		batch := missing[:min(len(missing), swarmFetchBatch)]
		missing = missing[len(batch):]

		if err = s.addFetched(ctx, src, batch); err != nil {
			return s.set.Head() - before, err
		}
	}

	return s.set.Head() - before, nil
}

// addFetched fetches one batch of IDs and adds the entries src answers among them.
func (s *swarmStore) addFetched(ctx *astral.Context, src *routerSwarmPeer, ids []*astral.ObjectID) error {
	want := make(map[astral.ObjectID]bool, len(ids))
	for _, id := range ids {
		want[*id] = true
	}

	objs, err := src.SwarmFetch(ctx, ids)
	if err != nil {
		return fmt.Errorf("swarm fetch: %w", err)
	}

	var firstErr error
	for _, obj := range objs {
		id, err := astral.ResolveObjectID(obj)
		// why: a peer answers only what was asked; anything else is ignored, not merged.
		if err != nil || !want[*id] {
			continue
		}

		// note: held and dropped entries are normal outcomes of a merge, not failures of the pull; a store that
		// fails is, and the pull reports the first one.
		err = s.merge(ctx, obj)
		if err != nil && !errors.Is(err, usermod.ErrSwarmEntryHeld) && !errors.Is(err, usermod.ErrSwarmEntryDropped) && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// routerSwarmPeer reaches another node's exchange ops through a router: the node's router in the running node.
type routerSwarmPeer struct {
	router astral.Router
	caller *astral.Identity
	target *astral.Identity
}

var _ swarmEntryPusher = &routerSwarmPeer{}

// open queries one exchange op on the target.
func (p *routerSwarmPeer) open(ctx *astral.Context, op string) (*channel.Channel, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	return query.Route(ctx, p.router, query.New(p.caller, p.target, usermod.ModuleName+"."+op, nil))
}

// SwarmDigest asks the target for the IDs of its stored entries.
func (p *routerSwarmPeer) SwarmDigest(ctx *astral.Context) (ids []*astral.ObjectID, err error) {
	ch, err := p.open(ctx, opSwarmDigest)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	err = ch.Switch(channel.Collect(&ids), channel.BreakOnEOS, channel.PassErrors, channel.FailOnEOF())

	return ids, err
}

// SwarmFetch asks the target for its stored entries among ids, at most swarmFetchBatch of them.
func (p *routerSwarmPeer) SwarmFetch(ctx *astral.Context, ids []*astral.ObjectID) (objs []astral.Object, err error) {
	ch, err := p.open(ctx, opSwarmFetch)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	for _, id := range ids {
		if err = ch.Send(id); err != nil {
			return nil, err
		}
	}
	if err = ch.Send(&astral.EOS{}); err != nil {
		return nil, err
	}

	err = ch.Switch(
		channel.BreakOnEOS,
		channel.PassErrors,
		func(obj astral.Object) error {
			objs = append(objs, obj)
			return nil
		},
		channel.FailOnEOF(),
	)

	return objs, err
}

// SwarmPush offers one entry to the target.
func (p *routerSwarmPeer) SwarmPush(ctx *astral.Context, obj astral.Object) error {
	ch, err := p.open(ctx, opSwarmPush)
	if err != nil {
		return err
	}
	defer ch.Close()

	if err = ch.Send(obj); err != nil {
		return err
	}

	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF())
}
