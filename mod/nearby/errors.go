package nearby

import "errors"

var ErrObjectTooLarge = errors.New("object too large")

// ErrInvalidMode refuses a nearby mode above nearby.ModeStealth.
var ErrInvalidMode = errors.New("invalid nearby mode")

// ErrStatusProofAttached refuses a composer's StatusProof: only the nearby module appends one.
var ErrStatusProofAttached = errors.New("a composer cannot attach a status proof")
