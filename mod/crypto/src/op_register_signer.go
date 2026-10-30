package crypto

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// signerKeysTimeout bounds the wait for the key set that opens a binding.
const signerKeysTimeout = 10 * time.Second

type opRegisterSignerArgs struct {
	In  string
	Out string
}

// OpRegisterSigner binds the caller as the signer of the keys it names for as
// long as the channel is open. After the ack the node sends signing requests and
// the app sends answers. Closing the channel, or any other object from the app,
// ends the binding.
//
// why the requests travel on this channel: a query back to the app needs an op
// gated on the caller being the node, and core.Router gives a token-less local
// query that identity (see AuthorizeSigner). With no such op there is no way to
// reach the signer past AuthorizeSigner.
//
// Spec: .ai/system/protocols/crypto/ops/crypto.register_signer.md
func (mod *Module) OpRegisterSigner(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterSignerArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out), channel.WithLockedWrites())
	defer ch.Close()

	// why: the origin is admitted by name, so an origin added later is refused
	// until it is judged.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	caller := q.Caller()
	// why: the router substitutes the node identity for a caller-less local query.
	if caller == nil || caller.IsZero() || caller.IsEqual(mod.node.Identity()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	keys, err := receiveSignerKeys(ch, signerKeysTimeout)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	pairs, err := mod.checkSignerKeys(keys)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if !mod.authorizeSignerKeys(ctx, caller, keys) {
		return ch.Send(auth.ErrNotPermitted)
	}

	binding := newSignerBinding(mod, caller, ch, pairs)
	defer binding.close()

	if err = mod.signers.claim(binding); err != nil {
		return ch.Send(astral.Err(err))
	}
	defer mod.signers.release(binding)

	err = ch.Send(&astral.Ack{})
	close(binding.ready)
	if err != nil {
		return err
	}

	// note: at the default level on purpose. This line is the operator's only
	// record of who claimed what.
	for _, entry := range keys.Keys {
		mod.log.Log("signer %v claims %v for %v", caller, entry.Key, entry.Scheme)
	}

	return ch.Switch(channel.WithContext(ctx), binding.receiveAnswer)
}

// receiveSignerKeys reads the first object of a binding. A channel that sends
// nothing in time is closed.
//
// why a timer and not channel.WithContext: the op goes on using this channel, and
// a context cancelled as Switch returns can race the goroutine watching it, which
// then closes the channel.
func receiveSignerKeys(ch *channel.Channel, timeout time.Duration) (*crypto.SignerKeys, error) {
	timer := time.AfterFunc(timeout, func() { _ = ch.Close() })
	defer timer.Stop()

	var keys *crypto.SignerKeys
	err := ch.Switch(channel.Expect(&keys), channel.FailOnEOF())
	return keys, err
}

// authorizeSignerKeys reports whether caller may claim every distinct key.
func (mod *Module) authorizeSignerKeys(ctx *astral.Context, caller *astral.Identity, keys *crypto.SignerKeys) bool {
	seen := make(map[string]struct{}, len(keys.Keys))

	for _, entry := range keys.Keys {
		if _, dup := seen[string(entry.Key.Key)]; dup {
			continue
		}
		seen[string(entry.Key.Key)] = struct{}{}

		if !mod.Auth.Authorize(ctx, &auth.RegisterSignerAction{Action: auth.NewAction(caller), Key: entry.Key}) {
			return false
		}
	}

	return true
}
