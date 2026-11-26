package nodes

import "github.com/astralp2p/astral-go/astral"

// These user fakes are unclaimed nodes to the SwarmState exchange: a link
// event offers and pulls nothing.
func (u *swarmNodeUser) Identity() *astral.Identity { return nil }
func (u *swarmUser) Identity() *astral.Identity     { return nil }
func (u *countingUser) Identity() *astral.Identity  { return nil }
