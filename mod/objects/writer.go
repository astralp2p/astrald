package objects

import "github.com/cryptopunkscc/astral-go/astral"

// Writer writes to objects created by Create(). Either Commit() or Discard() must be called at the end.
type Writer interface {
	// Write data to the object
	Write(p []byte) (n int, err error)

	// Commit commits the written data to storage and returns its ID. Closes the Writer.
	Commit() (*astral.ObjectID, error)

	// Discard the data written so far and close the Writer.
	Discard() error
}
