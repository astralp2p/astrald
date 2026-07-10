package indexing

import (
	"time"

	"github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

type opSubscribeArgs struct {
	Nonce astral.Nonce
	In    string `query:"optional"`
	Out   string `query:"optional"`
}

// OpSubscribe streams pending index/unindex changes to the caller in version
// order, waiting for an ack per change before advancing; retries on transient
// errors with exponential back-off and validates ack repo+version against the
// sent change before committing the cursor.
func (mod *Module) OpSubscribe(ctx *astral.Context, q *routing.IncomingQuery, args opSubscribeArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	indexer, err := mod.findIndexerByNonce(ctx, args.Nonce)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if indexer == nil {
		return ch.Send(astral.Err(indexing.ErrIndexNotFound))
	}

	retry, err := sig.NewRetry(time.Second, time.Minute, 2)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	var pendingRepo string
	var pendingChange *dbRepoEntry

	for {
		if pendingChange == nil {
			signal := mod.changeSignal()

			pendingRepo, pendingChange, err = mod.pickNextChange(ctx, indexer)
			if err != nil {
				return ch.Send(astral.Err(err))
			}

			if pendingChange == nil {
				// caught up — wait for a new change or ctx cancellation
				select {
				case <-signal:
					continue
				case <-ctx.Done():
					return nil
				}
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case <-retry.Retry():
			}
		}

		if pendingChange.Exist {
			err = ch.Send(&indexing.IndexMsg{
				Repo:     astral.String8(pendingRepo),
				Version:  astral.Uint64(pendingChange.Version),
				ObjectID: pendingChange.ObjectID,
			})
		} else {
			err = ch.Send(&indexing.UnindexMsg{
				Repo:     astral.String8(pendingRepo),
				Version:  astral.Uint64(pendingChange.Version),
				ObjectID: pendingChange.ObjectID,
			})
		}
		if err != nil {
			return err
		}

		var ack *indexing.ChangeAckMsg
		err = ch.Switch(
			channel.Expect(&ack),
			channel.PassErrors,
		)

		if err != nil {
			if indexing.IsIndexingTemporarilyFailed(err) {
				continue
			}

			mod.log.Logv(1, "indexer %v subscribe ended: %v", indexer.name, err)
			return nil
		}

		if string(ack.Repo) != pendingRepo || uint64(ack.Version) != pendingChange.Version {
			return ch.Send(astral.Err(indexing.ErrAckMismatch))
		}
		if err = mod.UpdateIndexerState(ctx, args.Nonce, pendingRepo, pendingChange.Version); err != nil {
			return ch.Send(astral.Err(err))
		}

		retry.Reset()
		pendingRepo = ""
		pendingChange = nil
	}
}
