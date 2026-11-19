package exonet

import "time"

// LinkKeepalive times the idle keepalive of a link over a transport's conns.
// A zero field leaves that value to the link layer's default.
//
// Spec: astral-docs topics/link-mux.md, Keepalive.
type LinkKeepalive struct {
	Idle    time.Duration // N: a link probes its peer after N without a received frame
	Timeout time.Duration // T: a link closes when its probe gets no pong within T
}

// KeepaliveConn is a Conn whose transport sets the keepalive of links over it.
type KeepaliveConn interface {
	LinkKeepalive() LinkKeepalive
}
