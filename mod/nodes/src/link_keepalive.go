package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// keepaliveOf returns the keepalive timing conn's transport sets, filling a value it leaves zero with the default.
func keepaliveOf(conn any) exonetmod.LinkKeepalive {
	var k exonetmod.LinkKeepalive
	if c, ok := conn.(exonetmod.KeepaliveConn); ok {
		k = c.LinkKeepalive()
	}
	if k.Idle <= 0 {
		k.Idle = defaultIdlePing
	}
	if k.Timeout <= 0 {
		k.Timeout = defaultPingTimeout
	}
	return k
}

// isProbe reports whether obj is a ping probe from the peer, as opposed to a pong.
func isProbe(obj astral.Object) bool {
	p, ok := obj.(*frames.Ping)
	return ok && !bool(p.Pong)
}

// stampRecv records that a frame other than a peer's probe arrived now.
//
// why a peer's probe does not count: each end then probes on its own
// schedule, so a path that delivers only one way is still detected from the
// end that hears nothing back.
func (s *Link) stampRecv(obj astral.Object) {
	if isProbe(obj) {
		return
	}
	s.lastRecv.Store(time.Now().UnixNano())
}

// idleLeft returns how long until the link has been silent for idlePing; zero or less means a probe is due.
func (s *Link) idleLeft() time.Duration {
	return time.Until(time.Unix(0, s.lastRecv.Load()).Add(s.idlePing))
}

// waitProbe blocks until a probe is due: the peer was silent for idlePing, or Wake was called.
// It returns false once the link is done.
func (s *Link) waitProbe(t *time.Timer) bool {
	for {
		wait := s.idleLeft()
		if wait <= 0 {
			return true
		}

		t.Reset(wait)
		select {
		case <-s.done:
			return false
		case <-s.wakeCh:
			return true
		case <-t.C:
			// note: a frame received while the timer ran moves the deadline, so
			// the loop re-arms instead of probing a link that is busy.
		}
	}
}
