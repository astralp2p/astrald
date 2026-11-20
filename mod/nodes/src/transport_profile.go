package nodes

import (
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// TransportProfile is every per-transport number that shapes link behaviour, keyed by Network() in transportProfiles.
type TransportProfile struct {
	// Rank orders transports by quality; higher is better, 0 is unranked.
	Rank int
	// IdlePing is the idle ping interval of a link whose conn sets none.
	IdlePing time.Duration
	// PingTimeout is the unanswered-ping timeout of a link whose conn sets none.
	PingTimeout time.Duration
	// RowBudget bounds one dial row's dial and handshake; 0 is no bound of its own.
	RowBudget time.Duration
	// Pressure configures the link pressure detector; nil attaches none.
	Pressure *LinkPressureConfig
}

// why: these apply only to a link whose transport sets no keepalive of its own
// (astral-docs topics/link-mux.md, Keepalive).
const (
	profileIdlePing    = 15 * time.Second
	profilePingTimeout = 10 * time.Second

	// why: a kcp link rides a UDP NAT mapping that survives 15 s of silence on
	// common NATs but not 30 s; mod/kcp sets the same 10 s on its conns
	// (astrald#537), so the profile matches what a kcp link runs with.
	kcpIdlePing = 10 * time.Second
)

// fallbackProfile applies to a network without an entry in transportProfiles.
var fallbackProfile = TransportProfile{
	IdlePing:    profileIdlePing,
	PingTimeout: profilePingTimeout,
}

// transportProfiles holds one profile per network the module links over.
//
// why RowBudget kcp: a kcp dial succeeds at once over UDP, so a dead row fails only in the Noise handshake, which otherwise only the kcp dial_timeout bounds.
// why RowBudget gw: a gw dial routes a query to the gateway, which may start a nested dial round for the gateway itself.
// why no RowBudget on tcp and tor: the dial of their own bounds it; tor retries under its own timeouts.
var transportProfiles = map[string]TransportProfile{
	"tcp": {Rank: 50, IdlePing: profileIdlePing, PingTimeout: profilePingTimeout},
	"kcp": {Rank: 40, IdlePing: kcpIdlePing, PingTimeout: profilePingTimeout, RowBudget: 5 * time.Second},
	"gw":  {Rank: 20, IdlePing: profileIdlePing, PingTimeout: profilePingTimeout, RowBudget: 20 * time.Second},
	"tor": {Rank: 10, IdlePing: profileIdlePing, PingTimeout: profilePingTimeout, Pressure: &TorLinkPressureConfig},
}

// profileOf returns the profile of network, or fallbackProfile when it has none.
func profileOf(network string) TransportProfile {
	if p, ok := transportProfiles[network]; ok {
		return p
	}
	return fallbackProfile
}

// rankOf returns the rank of network; an unknown network ranks 0.
func rankOf(network string) int {
	return transportProfiles[network].Rank
}

// rowBudgetsOf returns the dial row budget of each of networks that has one.
func rowBudgetsOf(networks []string) map[string]time.Duration {
	budgets := map[string]time.Duration{}
	for _, n := range networks {
		if d := transportProfiles[n].RowBudget; d > 0 {
			budgets[n] = d
		}
	}
	return budgets
}

// newPressureDetector returns the pressure detector network's profile configures, or nil when it configures none.
func newPressureDetector(network string, now time.Time, onHigh func()) LinkPressureDetector {
	cfg := transportProfiles[network].Pressure
	if cfg == nil {
		return nil
	}
	return NewLinkPressureDetector(now, *cfg, onHigh)
}

// networkOf returns the network conn reports through its endpoints, or "unknown".
func networkOf(conn any) string {
	if c, ok := conn.(exonetmod.Conn); ok {
		if e := c.RemoteEndpoint(); e != nil {
			return e.Network()
		}
		if e := c.LocalEndpoint(); e != nil {
			return e.Network()
		}
	}
	return "unknown"
}

// NATKeepalive is the wake schedule of a NAT-punched link: it wakes every Fresh
// while the punch is younger than FreshFor, then every Idle.
//
// why a strategy setting and not a profile entry: a punched link is kcp by
// Network(), so the NAT strategy, not the rank, knows it is punched.
// why Idle is below 25 s: common UDP mapping timeouts start near 30 s.
type NATKeepalive struct {
	Fresh    time.Duration
	FreshFor time.Duration
	Idle     time.Duration
}

var natKeepalive = NATKeepalive{Fresh: 5 * time.Second, FreshFor: time.Minute, Idle: 15 * time.Second}

// wakeClock is the time source of the NAT wake loop.
type wakeClock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// natWakeLoop calls wake on cfg's schedule, counted from the call, until done closes.
func natWakeLoop(clock wakeClock, cfg NATKeepalive, wake func(), done <-chan struct{}) {
	start := clock.Now()
	for {
		interval := cfg.Idle
		if clock.Now().Sub(start) < cfg.FreshFor {
			interval = cfg.Fresh
		}
		select {
		case <-clock.After(interval):
			wake()
		case <-done:
			return
		}
	}
}
