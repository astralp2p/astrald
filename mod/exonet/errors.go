package exonet

import (
	"errors"

	"github.com/astralp2p/astral-go/api/exonet"
)

// ErrUnsupportedNetwork is declared in astral-go, beside the Endpoint it refuses, so clients can match it.
var ErrUnsupportedNetwork = exonet.ErrUnsupportedNetwork
var ErrDisabledNetwork = errors.New("disabled network")
