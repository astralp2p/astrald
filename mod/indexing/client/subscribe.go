package indexing

import (
	"errors"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/indexing"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Subscription is an open indexing.subscribe stream.
// Each Next must be followed by exactly one ChangeAckMsg or a temporary failure.
type Subscription struct {
	ch      *channel.Channel
	pending astral.Object
	closeMu sync.Once
}

// Subscribe opens a live change stream for the indexer identified by nonce.
// A background goroutine closes the subscription when ctx is done.
func (c *Client) Subscribe(ctx *astral.Context, nonce astral.Nonce) (*Subscription, error) {
	ch, err := c.queryCh(ctx, indexing.MethodSubscribe, query.Args{
		"nonce": nonce,
	})
	if err != nil {
		return nil, err
	}

	sub := &Subscription{ch: ch}

	go func() {
		<-ctx.Done()
		_ = sub.Close()
	}()

	return sub, nil
}

func Subscribe(ctx *astral.Context, nonce astral.Nonce) (*Subscription, error) {
	return Default().Subscribe(ctx, nonce)
}

// Next returns the next change object. It blocks until the server sends one
// or the channel closes.
func (s *Subscription) Next() (astral.Object, error) {
	if s.pending != nil {
		return nil, errors.New("previous change not acknowledged")
	}

	obj, err := s.ch.Receive()
	if err != nil {
		return nil, err
	}

	switch o := obj.(type) {
	case *indexing.IndexMsg:
		s.pending = o
	case *indexing.UnindexMsg:
		s.pending = o
	case astral.Error:
		return nil, o
	default:
		return nil, astral.NewErrUnexpectedObject(obj)
	}

	return obj, nil
}

// Ack confirms the last Next and advances the indexer's cursor.
func (s *Subscription) Ack() error {
	if s.pending == nil {
		return errors.New("no pending change to ack")
	}

	var repo astral.String8
	var version astral.Uint64

	switch pending := s.pending.(type) {
	case *indexing.IndexMsg:
		repo = pending.Repo
		version = pending.Version
	case *indexing.UnindexMsg:
		repo = pending.Repo
		version = pending.Version
	default:
		return astral.NewErrUnexpectedObject(s.pending)
	}

	err := s.ch.Send(&indexing.ChangeAckMsg{
		Repo:    repo,
		Version: version,
	})
	if err != nil {
		return err
	}
	s.pending = nil
	return nil
}

// Fail rejects the last Next without advancing the cursor; the server keeps the
// subscription open so the same change can be retried.
func (s *Subscription) Fail() error {
	if s.pending == nil {
		return errors.New("no pending change to fail")
	}

	err := s.ch.Send(indexing.ErrIndexingTemporarilyFailed)
	if err != nil {
		return err
	}
	s.pending = nil
	return nil
}

func (s *Subscription) Close() error {
	var err error
	s.closeMu.Do(func() {
		err = s.ch.Close()
	})
	return err
}
