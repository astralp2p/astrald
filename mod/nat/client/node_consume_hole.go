package nat

import (
	"github.com/cryptopunkscc/astral-go/api/nat"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// NodeConsumeHole claims a hole identified by pair; when target is nil the caller acts as responder and drives
// the lock/take handshake, otherwise it acts as initiator and waits for an ack.
func (client *Client) NodeConsumeHole(ctx *astral.Context, pair astral.Nonce, target *astral.Identity) error {
	args := query.Args{"pair": pair}
	if target != nil {
		args["target"] = target.String()
	}

	ch, err := client.queryCh(ctx, nat.MethodNodeConsumeHole, args)
	if err != nil {
		return err
	}
	defer ch.Close()

	if target != nil {
		// Initiator: wait for Ack
		return ch.Switch(
			channel.ExpectAck,
			func(msg *astral.ErrorMessage) error { return msg },
			channel.WithContext(ctx),
		)
	}

	// Responder: drive the handshake
	if err := ch.Send(&nat.ConsumeHoleSignal{Signal: nat.ConsumeHoleSignalTypeLock, Pair: pair}); err != nil {
		return err
	}

	err = ch.Switch(
		nat.ExpectConsumeHoleSignal(pair, nat.ConsumeHoleSignalTypeLocked, nat.HandleFailedConsumeHoleSignal),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
	if err != nil {
		return err
	}

	if err := ch.Send(&nat.ConsumeHoleSignal{Signal: nat.ConsumeHoleSignalTypeTake, Pair: pair}); err != nil {
		return err
	}

	return ch.Switch(
		nat.ExpectConsumeHoleSignal(pair, nat.ConsumeHoleSignalTypeTaken, nat.HandleFailedConsumeHoleSignal),
		channel.PassErrors,
		channel.WithContext(ctx),
	)
}
