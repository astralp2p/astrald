package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCarryJoinArgs struct {
	In  string
	Out string
}

// OpCarryJoin carries the caller's JoinRequest into SwarmState and signs its
// membership with the caller in this session (swarmCarrier.Carry). It reads
// one mod.user.signed_join_request, which must name the caller as Joiner and
// this node as Carrier, sends the membership its issuer signed, reads it back
// signed by the joiner, stores it, and sends it with both signatures, then the
// user identity the membership derives from. A Decline, the request's Timeout,
// or a refusal sends an error whose text names the end (joinEndOf); a dropped
// connection only pauses the join.
// Spec: .ai/system/protocols/user/ops/user.carry_join.md
//
// why no named action: the caller is the unclaimed joiner, which holds
// nothing; the carrier's tier decides what it signs.
func (mod *Module) OpCarryJoin(ctx *astral.Context, q *routing.IncomingQuery, args opCarryJoinArgs) error {
	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if mod.ActiveContract() == nil {
		return ch.Send(user.ErrNoActiveContract)
	}

	var req *user.SignedJoinRequest
	if err := ch.Switch(channel.Expect(&req), channel.FailOnEOF()); err != nil {
		return ch.Send(astral.Err(err))
	}
	if req.IsNil() || !req.Joiner.IsEqual(q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: the op runs on a context detached from the joiner's, and only the
	// conn tells the wait that the joiner left.
	waitCtx, stop := routing.UntilDisconnect(ctx.IncludeZone(astral.ZoneNetwork), conn)
	defer stop()

	// the joiner's side of the session: it gets the membership its issuer
	// signed and answers it signed by the joiner.
	cosign := func(_ *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
		if err := ch.Send(half); err != nil {
			return nil, err
		}
		var signed *auth.SignedContract
		if err := ch.Switch(channel.Expect(&signed), channel.FailOnEOF()); err != nil {
			return nil, errNotSignedByJoiner
		}
		return signed, nil
	}

	sc, err := mod.carrier.Carry(waitCtx, req, mod.userKeyCeremony(), cosign)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if err = ch.Send(sc); err != nil {
		return err
	}
	return ch.Send(mod.Identity())
}

// joinEndOf returns the end of a join msg names: the end of the same text.
func joinEndOf(msg *astral.ErrorMessage) error { return joinEnd(msg.Error()) }
