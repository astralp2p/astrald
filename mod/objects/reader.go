package objects

import "github.com/astralp2p/astral-go/astral"

// Reader reads object opened by Read()
type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
	Repo() Repository

	// ID returns the full ID of the whole opened object, also when the read window is a slice.
	// ID is never nil on a reader that Read returned.
	// ID returns a copy, and the caller owns it.
	// why: a partial ID resolves inside the repository, so only the returned reader knows the full ID it found.
	ID() *astral.ObjectID
}
