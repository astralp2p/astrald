package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

type writer struct {
	ch *channel.Channel
}

func (w *writer) Write(p []byte) (n int, err error) {
	err = w.ch.Send((*astral.Blob)(&p))
	if err == nil {
		n = len(p)
	}
	return
}

func (w *writer) Commit() (*astral.ObjectID, error) {
	// close the channel after committing
	defer w.ch.Close()

	// send commit message
	err := w.ch.Send(&objects.CommitMsg{})
	if err != nil {
		return nil, err
	}

	// handle response
	o, err := w.ch.Receive()
	switch msg := o.(type) {
	case *astral.ObjectID:
		return msg, nil
	case *astral.ErrorMessage:
		return nil, msg
	case nil:
		return nil, err
	default:
		return nil, astral.NewErrUnexpectedObject(msg)
	}
}

func (w *writer) Discard() error {
	return w.ch.Close() // close without committing to discard data
}
