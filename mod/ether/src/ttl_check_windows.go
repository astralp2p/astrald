package ether

// checkInboundTTL turns on the drop of a datagram that did not arrive with linkTTL.
// why: a Windows socket cannot report the IP TTL of a received datagram; a Windows node still sends with linkTTL.
const checkInboundTTL = false
