//go:build !windows

package ether

// checkInboundTTL turns on the drop of a datagram that did not arrive with linkTTL.
const checkInboundTTL = true
