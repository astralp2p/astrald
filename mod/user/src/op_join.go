package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// joinWait bounds the wait of user.join for a route to the swarm node.
//
// why: a node answers a scan within its 2-second reply window
// (mod/nearby/src/scan_reply.go), so the reply arrives within the wait.
const joinWait = 3 * time.Second

// joinPoll is how often user.join checks for a route during joinWait.
const joinPoll = 250 * time.Millisecond

// errNoCarrierMembership is the end of a join whose carrier answered no membership.
var errNoCarrierMembership = errors.New("carrier answered no membership")

type opJoinArgs struct {
	Node string `query:"required"`
	Tier string
	In   string
	Out  string
}

// OpJoin makes this unclaimed node ask to join the swarm through Node, its
// carrier: it signs a JoinRequest naming Node and Tier (member by default),
// keeps it until the join ends, and sends it to Node's user.carry_join. The
// carrier writes it into SwarmState and, while this node stays connected,
// answers the membership its issuer signed: the carrier, or for
// privileged_member the user key in a ceremony. This node co-signs it, and
// activates it once the carrier confirms it stored the contract. A dropped
// connection keeps the request: the next user.join through the same carrier
// resends the same request. The reply is the membership.
// Spec: .ai/system/protocols/user/ops/user.join.md
//
// why: no named action guards this op. It claims this node, so it admits the
// queries user.bootstrap admits: only the node itself or the manager may claim
// an unclaimed node (mayClaim).
func (mod *Module) OpJoin(ctx *astral.Context, q *routing.IncomingQuery, args opJoinArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if mod.ActiveContract() != nil {
		return ch.Send(user.ErrAlreadyClaimed)
	}

	if !mod.mayClaim(q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	tier := user.TierMember
	if args.Tier != "" {
		tier = user.Tier(args.Tier)
	}
	if !tier.IsValid() {
		return ch.Send(astral.NewError("invalid tier"))
	}

	nodeID, err := mod.Dir.ResolveIdentity(args.Node)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if nodeID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}
	if nodeID.IsEqual(mod.node.Identity()) {
		return ch.Send(user.ErrJoinSelf)
	}

	// why: the request is this node's own query, and a session without a token
	// has the Network zone stripped (mod/apphost/src/guest.go).
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	// why CompareAndSwap: a later join that replaced the target keeps it.
	// note: concurrent joins share one target, the last one set.
	mod.joinTarget.Store(nodeID)
	defer mod.joinTarget.CompareAndSwap(nodeID, nil)
	mod.findRoute(ctx, nodeID)

	req, err := mod.joiner.Request(ctx, nodeID, tier)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// the carrier session: send req to the carrier's user.carry_join and run the
	// joiner's side of it (joinSession).
	var root *astral.Identity
	signed, err := mod.joiner.Join(ctx, req, func(ctx *astral.Context, req *user.SignedJoinRequest, cosign joinCosign) (*auth.SignedContract, error) {
		carrier, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), req.Carrier, usermod.ModuleName+".carry_join", nil))
		if err != nil {
			return nil, err
		}
		defer carrier.Close()

		sc, id, err := joinSession(ctx, carrier, req, cosign)
		root = id
		return sc, err
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.activateJoin(ctx, signed, root, nodeID); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}

// joinSession is the joiner's side of a carrier session over ch: it sends
// req, sends back the membership the carrier answers signed by cosign, and
// returns the membership the carrier confirms with both signatures and the
// user identity it answers. An error_message ends the join (joinEndOf).
// Spec: .ai/system/protocols/user/ops/user.carry_join.md
func joinSession(ctx *astral.Context, ch *channel.Channel, req *user.SignedJoinRequest, cosign joinCosign) (*auth.SignedContract, *astral.Identity, error) {
	if err := ch.Send(req); err != nil {
		return nil, nil, err
	}

	var sc *auth.SignedContract
	var root *astral.Identity
	cosigned := false
	err := ch.Switch(
		joinEndOf,
		func(c *auth.SignedContract) error {
			if cosigned {
				sc = c
				return nil
			}
			signed, err := cosign(ctx, c)
			if err != nil {
				return err
			}
			cosigned = true
			return ch.Send(signed)
		},
		func(id *astral.Identity) error { root = id; return channel.ErrBreak },
		channel.FailOnEOF(),
	)
	switch {
	case err != nil:
		return nil, nil, err
	case sc == nil || root.IsZero():
		return nil, nil, errNoCarrierMembership
	}
	return sc, root, nil
}

// activateJoin stores root as this node's user, claims the node with sc, adds
// sc to SwarmState and pulls the rest of SwarmState from carrier.
//
// why: the user is stored before the claim, because the claim makes the root key
// mod.Identity() and the entry check judges sc against it.
func (mod *Module) activateJoin(ctx *astral.Context, sc *auth.SignedContract, root, carrier *astral.Identity) error {
	if err := mod.validateActiveContract(sc); err != nil {
		return err
	}
	if err := mod.config.User.Set(mod.ctx, root); err != nil {
		return err
	}
	if err := mod.claim(ctx, sc); err != nil {
		return err
	}

	// note: sc is held until the pull brings its carrier's membership.
	_ = mod.AddContract(ctx, sc)
	if err := mod.SyncSwarm(ctx, carrier); err != nil {
		mod.log.Logv(1, "join: swarm state pull from %v: %v", carrier, err)
	}
	return nil
}

// findRoute gives user.join a route to nodeID on the local network. Unless the
// node is linked or resolves an endpoint, it scans with nearby and waits up to
// joinWait, bounded by ctx, for an endpoint. It returns without a route when
// none appears; the request then fails with route not found.
// Spec: .ai/system/protocols/user/ops/user.join.md
func (mod *Module) findRoute(ctx *astral.Context, nodeID *astral.Identity) {
	if mod.hasRoute(ctx, nodeID) {
		return
	}
	if err := mod.Nearby.Scan(); err != nil {
		mod.log.Logv(1, "join %v: nearby scan: %v", nodeID, err)
	}

	wait, cancel := ctx.WithTimeout(joinWait)
	defer cancel()
	poll := time.NewTicker(joinPoll)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if mod.hasRoute(wait, nodeID) {
				return
			}
		case <-wait.Done():
			mod.log.Logv(1, "join %v: no route after %v", nodeID, joinWait)
			return
		}
	}
}

// hasRoute reports whether nodeID is linked or resolves at least one endpoint.
func (mod *Module) hasRoute(ctx *astral.Context, nodeID *astral.Identity) bool {
	if mod.Nodes.IsLinked(nodeID) {
		return true
	}

	// why cancel: the resolvers stop sending once the first endpoint is read.
	ctx, cancel := ctx.WithCancel()
	defer cancel()
	endpoints, err := mod.Nodes.ResolveEndpoints(ctx, nodeID)
	if err != nil {
		return false
	}
	_, ok := <-endpoints
	return ok
}
