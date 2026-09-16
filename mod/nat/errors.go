package nat

import "errors"

var ErrDuplicateHole = errors.New("duplicate hole")
var ErrHoleNotExists = errors.New("hole not exists")
var ErrHoleBusy = errors.New("hole is busy")
var ErrHoleCantLock = errors.New("hole can't lock")
var ErrNoSuitableIP = errors.New("no suitable IPv4 address found")
var ErrUnknownIdentity = errors.New("unknown identity")
