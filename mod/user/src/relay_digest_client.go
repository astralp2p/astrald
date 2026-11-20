package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/core"
)

// relayDigestTimeout bounds one digest query, apart from the per-push
// siblingPushTimeout.
const relayDigestTimeout = 10 * time.Second

// errDigestUnfinished is a digest stream that ended without EOS.
var errDigestUnfinished = errors.New("relay digest ended without eos")

// relayDigest is the set of relay contract object IDs a sibling holds.
type relayDigest map[astral.ObjectID]struct{}

// has reports whether the digest lists id.
func (d relayDigest) has(id *astral.ObjectID) bool {
	_, ok := d[*id]
	return ok
}

// askDigestFunc asks sibling which relay contracts it holds.
type askDigestFunc func(ctx *astral.Context, sibling *astral.Identity) (relayDigest, error)

// fetchRelayDigest asks sibling for its relay digest (user.relay_digest) within
// its own relayDigestTimeout. Any failure returns an error and no digest: an
// error, a refusal, an unknown op, a stream without EOS, or the deadline.
func (mod *Module) fetchRelayDigest(ctx *astral.Context, sibling *astral.Identity) (relayDigest, error) {
	timeout := mod.digestTimeout
	if timeout <= 0 {
		timeout = relayDigestTimeout
	}
	dctx, cancel := ctx.WithTimeout(timeout)
	defer cancel()

	ask := mod.askDigest
	if ask == nil {
		ask = mod.askRelayDigest
	}

	digest, err := ask(dctx, sibling)
	if err == nil && dctx.Err() != nil {
		// why: a read the deadline cut short can look like a clean end.
		err = dctx.Err()
	}
	if err != nil {
		return nil, err
	}

	return digest, nil
}

// askRelayDigest asks sibling with user.relay_digest as this node.
// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
func (mod *Module) askRelayDigest(ctx *astral.Context, sibling *astral.Identity) (relayDigest, error) {
	client := core.NewClientAs(mod.node, mod.node.Identity()).WithTarget(sibling)
	ch, err := client.QueryChannel(ctx.WithIdentity(mod.node.Identity()), relayDigestOp, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	var ids []*astral.ObjectID
	var sawEOS bool
	err = ch.Switch(
		channel.Collect(&ids),
		channel.MarkEOS(&sawEOS),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if !sawEOS {
		return nil, errDigestUnfinished
	}

	digest := make(relayDigest, len(ids))
	for _, id := range ids {
		digest[*id] = struct{}{}
	}

	return digest, nil
}
