package nodes

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// The SwarmState exchange between two nodes, over queries to the nodes module:
//
//	swarm_digest  answers the ID of every stored entry, then EOS.
//	swarm_fetch   receives entry IDs until EOS, answers each stored entry among them, then EOS.
//	swarm_push    receives one entry and answers Ack when it is stored or was already, or the error.
//
// A node pulls what it lacks (digest, then fetch of the missing IDs) and pushes each entry it stores to its peers.
// Every entry goes through AddSwarmEntry on the receiving node, so a merge from a peer is checked like a local write.
//
// note: nothing registers these ops or starts a pull yet; G7 does both.
const (
	opSwarmDigest = "swarm_digest"
	opSwarmFetch  = "swarm_fetch"
	opSwarmPush   = "swarm_push"

	// swarmFetchBatch is the most IDs one swarm_fetch asks for.
	swarmFetchBatch = 256
)

// swarmSource is a peer's copy of SwarmState as a pull reads it.
type swarmSource interface {
	SwarmDigest(ctx *astral.Context) ([]*astral.ObjectID, error)
	SwarmFetch(ctx *astral.Context, ids []*astral.ObjectID) ([]astral.Object, error)
}

// pullSwarm fetches the entries src stores and this node lacks, and adds each one. It returns how much the head
// grew. Entries that wait are held; entries that are dropped are not stored.
func (s *swarmStore) pullSwarm(ctx *astral.Context, src swarmSource) (uint64, error) {
	before := s.SwarmHead()

	digest, err := src.SwarmDigest(ctx)
	if err != nil {
		return 0, fmt.Errorf("swarm digest: %w", err)
	}

	var missing []*astral.ObjectID
	for _, id := range digest {
		if id != nil && !s.isStored(id) {
			missing = append(missing, id)
		}
	}

	for len(missing) > 0 {
		batch := missing[:min(len(missing), swarmFetchBatch)]
		missing = missing[len(batch):]

		if err = s.addFetched(ctx, src, batch); err != nil {
			return s.SwarmHead() - before, err
		}
	}

	return s.SwarmHead() - before, nil
}

// addFetched fetches one batch of IDs and adds the entries src answers among them.
func (s *swarmStore) addFetched(ctx *astral.Context, src swarmSource, ids []*astral.ObjectID) error {
	want := make(map[astral.ObjectID]bool, len(ids))
	for _, id := range ids {
		want[*id] = true
	}

	objs, err := src.SwarmFetch(ctx, ids)
	if err != nil {
		return fmt.Errorf("swarm fetch: %w", err)
	}

	for _, obj := range objs {
		id, err := astral.ResolveObjectID(obj)
		// why: a peer answers only what was asked; anything else is ignored, not merged.
		if err != nil || !want[*id] {
			continue
		}

		// note: held and dropped entries are normal outcomes of a merge, not failures of the pull.
		_ = s.AddSwarmEntry(ctx, obj)
	}

	return nil
}

// swarmOpAdder is where addSwarmOps puts the ops: the nodes module's op router.
type swarmOpAdder interface {
	AddOp(name string, op *routing.Op) error
}

// swarmOps answers the exchange ops for one store.
type swarmOps struct {
	store *swarmStore
	// admit decides which callers may read or write the set.
	admit func(caller *astral.Identity) bool
}

// addSwarmOps adds the exchange ops over store to router. admit decides which callers each op serves.
func addSwarmOps(router swarmOpAdder, store *swarmStore, admit func(caller *astral.Identity) bool) error {
	ops := &swarmOps{store: store, admit: admit}

	for name, fn := range map[string]any{
		opSwarmDigest: ops.digest,
		opSwarmFetch:  ops.fetch,
		opSwarmPush:   ops.push,
	} {
		op, err := routing.NewOp(fn)
		if err != nil {
			return err
		}
		if err = router.AddOp(name, op); err != nil {
			return fmt.Errorf("add %v: %w", name, err)
		}
	}

	return nil
}

// digest answers swarm_digest.
func (o *swarmOps) digest(ctx *astral.Context, q *routing.IncomingQuery) error {
	if !o.admit(q.Caller()) {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	for _, id := range o.store.swarmDigest() {
		if err := ch.Send(id); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

// fetch answers swarm_fetch. An ID with no stored entry is skipped.
func (o *swarmOps) fetch(ctx *astral.Context, q *routing.IncomingQuery) error {
	if !o.admit(q.Caller()) {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	var ids []*astral.ObjectID
	err := ch.Switch(
		func(id *astral.ObjectID) error {
			if len(ids) == swarmFetchBatch {
				return errors.New("too many IDs")
			}
			ids = append(ids, id)
			return nil
		},
		channel.BreakOnEOS,
		channel.FailOnEOF(),
		channel.WithContext(ctx),
	)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, id := range ids {
		obj, err := o.store.SwarmEntry(id)
		if err != nil {
			continue
		}
		if err = ch.Send(obj); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

// push answers swarm_push.
func (o *swarmOps) push(ctx *astral.Context, q *routing.IncomingQuery) error {
	if !o.admit(q.Caller()) {
		return q.Reject()
	}

	ch := q.Accept()
	defer ch.Close()

	obj, err := ch.Receive()
	if err != nil {
		return err
	}

	if err = o.store.AddSwarmEntry(ctx, obj); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// routerSwarmPeer reaches another node's exchange ops through a router: the node's router in the running node.
type routerSwarmPeer struct {
	router astral.Router
	caller *astral.Identity
	target *astral.Identity
}

var (
	_ swarmSource = &routerSwarmPeer{}
	_ swarmPusher = &routerSwarmPeer{}
)

// open queries one exchange op on the target.
func (p *routerSwarmPeer) open(ctx *astral.Context, op string) (*channel.Channel, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	return query.Route(ctx, p.router, query.New(p.caller, p.target, nodesmod.ModuleName+"."+op, nil))
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
