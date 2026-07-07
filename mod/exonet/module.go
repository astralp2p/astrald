package exonet

import (
	"context"

	"github.com/cryptopunkscc/astral-go/astral"
)

const ModuleName = "exonet"

// Module is the exonet service: a per-network registry of dialers, unpackers,
// and parsers, plus dispatch methods that route to the registered handler for
// the target network. SetDialer/SetUnpacker/SetParser replace any existing
// registration for that network name.
type Module interface {
	Dial(*astral.Context, Endpoint) (conn Conn, err error)
	Unpack(network string, data []byte) (Endpoint, error)
	Parse(network string, address string) (Endpoint, error)

	SetDialer(network string, dialer Dialer)
	SetUnpacker(network string, unpacker Unpacker)
	SetParser(network string, parser Parser)
}

// Endpoint represents a dialable address on a network (such as an IP address with port number)
type Endpoint interface {
	astral.Object
	Network() string // network name
	Address() string // text representation of the address
	Pack() []byte    // binary representation of the address
}

type Dialer interface {
	Dial(*astral.Context, Endpoint) (Conn, error)
}

type Unpacker interface {
	Unpack(network string, data []byte) (Endpoint, error)
}

type Parser interface {
	Parse(network string, address string) (Endpoint, error)
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
