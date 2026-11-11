package main

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// The two types no SDK ships. A process that never calls registerTypes does
// not know them: the consumer is such a process.
const (
	offerType   = "app_blueprint_sync.offer"
	readingType = "app_blueprint_sync.reading"
)

// Offer is the info an offering carries.
type Offer struct {
	Label astral.String16
	Tier  astral.Uint8
}

// astral:blueprint-ignore registered by registerTypes, in the provider process only
func (*Offer) ObjectType() string { return offerType }

func (o Offer) WriteTo(w io.Writer) (int64, error) { return astral.Objectify(&o).WriteTo(w) }

func (o *Offer) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(o).ReadFrom(r) }

// Reading is the typed reply of the reading op.
type Reading struct {
	Label  astral.String16
	Serial astral.Uint64
}

// astral:blueprint-ignore registered by registerTypes, in the provider process only
func (*Reading) ObjectType() string { return readingType }

func (o Reading) WriteTo(w io.Writer) (int64, error) { return astral.Objectify(&o).WriteTo(w) }

func (o *Reading) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(o).ReadFrom(r) }

const (
	offerLabel    = "a type no SDK ships"
	offerTier     = 7
	readingLabel  = "typed reply of its own"
	readingSerial = 20261110
)

// registerTypes adds the types to this process's compile-time registry, which
// is what apps.SyncBlueprints derives the blueprints it pushes from.
func registerTypes() error { return astral.Add(&Offer{}, &Reading{}) }
