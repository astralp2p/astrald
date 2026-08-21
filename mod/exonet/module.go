package exonet

import (
	"context"
	"github.com/astralp2p/astral-go/api/exonet"

	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "exonet"

// Module is the exonet service: a per-network registry of dialers, unpackers,
// and parsers, plus dispatch methods that route to the registered handler for
// the target network. SetDialer/SetUnpacker/SetParser replace any existing
// registration for that network name.
type Module interface {
	Dial(*astral.Context, exonet.Endpoint) (conn Conn, err error)
	Unpack(network string, data []byte) (exonet.Endpoint, error)
	Parse(network string, address string) (exonet.Endpoint, error)

	SetDialer(network string, dialer Dialer)
	SetUnpacker(network string, unpacker Unpacker)
	SetParser(network string, parser Parser)
}

type Dialer interface {
	Dial(*astral.Context, exonet.Endpoint) (Conn, error)
}

type Unpacker interface {
	Unpack(network string, data []byte) (exonet.Endpoint, error)
}

type Parser interface {
	Parse(network string, address string) (exonet.Endpoint, error)
}

// EphemeralHandler processes a single inbound connection; returning stopListener=true
// instructs the owning EphemeralListener to stop accepting further connections.
type EphemeralHandler func(ctx context.Context, conn Conn) (stopListener bool, err error)

// EphemeralListener accepts connections until its handler signals stop or the
// context is cancelled. Run blocks until the listener is done; Close tears it
// down early from another goroutine.
type EphemeralListener interface {
	Run(ctx *astral.Context) error
	Close() error
}
