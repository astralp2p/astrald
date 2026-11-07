package nearby

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
)

// replyWindow is the shortest time between two scan replies on one subnet.
//
// why: a scan is a broadcast anyone on the subnet can send, and every node answers it
// with a broadcast; the window bounds that reflection to one status per subnet.
const replyWindow = 2 * time.Second

// maxReplySubnets bounds the subnets with reply state at one time.
//
// note: the keys are local subnets, so the bound is never reached on a real host.
const maxReplySubnets = 64

// The ways to answer a scan.
const (
	replyNone  = iota // note: a reply is already sent or pending in this window
	replyNow          // note: the window is over: reply at once
	replyLater        // note: reply once, at the end of the window
)

// scanReplies holds the scan reply window of each local subnet.
//
// why: a mutex, not sig.Map: the window check and its update are one step.
type scanReplies struct {
	mu      sync.Mutex
	subnets map[string]*subnetReply
}

// subnetReply is the reply state of one subnet.
type subnetReply struct {
	last    time.Time // note: the time of the last reply
	pending bool      // note: a deferred reply is scheduled
}

// plan decides how to answer a scan on subnet at now. For replyLater it returns the
// wait until the end of the window.
func (r *scanReplies) plan(subnet string, now time.Time) (action int, wait time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.prune(now)
	s, ok := r.subnets[subnet]
	switch {
	case ok && s.pending:
		return replyNone, 0
	case ok:
		s.pending = true
		return replyLater, s.last.Add(replyWindow).Sub(now)
	case len(r.subnets) >= maxReplySubnets:
		return replyNone, 0
	}

	if r.subnets == nil {
		r.subnets = make(map[string]*subnetReply)
	}
	r.subnets[subnet] = &subnetReply{last: now}
	return replyNow, 0
}

// sent records the deferred reply on subnet as sent at now.
func (r *scanReplies) sent(subnet string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.subnets == nil {
		r.subnets = make(map[string]*subnetReply)
	}
	r.subnets[subnet] = &subnetReply{last: now}
}

// prune drops the state of every subnet whose window is over and has no reply pending.
//
// note: such a subnet answers its next scan at once, exactly as one with no state.
func (r *scanReplies) prune(now time.Time) {
	for subnet, s := range r.subnets {
		if !s.pending && now.Sub(s.last) >= replyWindow {
			delete(r.subnets, subnet)
		}
	}
}

// replyToScan answers a scan from src by broadcasting the status on the subnet that
// contains src only, at most once per replyWindow. It never sends from the caller's
// goroutine.
func (mod *Module) replyToScan(src ip.IP) {
	if mod.Mode() == nearby.ModeSilent {
		return
	}

	subnet, ok := mod.Ether.SubnetOf(src)
	if !ok {
		return
	}

	switch action, wait := mod.replies.plan(subnet, mod.now()); action {
	case replyNow:
		go mod.pushScanReply(src)
	case replyLater:
		go mod.deferScanReply(subnet, src, wait)
	}
}

// deferScanReply sends the scan reply on subnet once wait has passed.
func (mod *Module) deferScanReply(subnet string, src ip.IP, wait time.Duration) {
	<-mod.after(wait)
	mod.replies.sent(subnet, mod.now())
	mod.pushScanReply(src)
}

// pushScanReply broadcasts the status on the subnet that contains src.
func (mod *Module) pushScanReply(src ip.IP) {
	s, err := mod.status()
	if err != nil {
		mod.log.Error("scan reply: %v", err)
		return
	}
	if s == nil {
		return
	}
	if err = mod.Ether.PushToSubnetOf(src, s); err != nil {
		mod.log.Logv(1, "scan reply to %v: %v", src, err)
	}
}
