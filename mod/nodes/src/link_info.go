package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// srttWeight is the share of each new round trip in the smoothed round trip (RFC 6298, alpha 1/8).
const srttWeight = 8

// recordRTT folds a pong's round trip into the link's smoothed round trip.
// note: pongs are handled one at a time, so recordRTT has a single writer.
// why on the link: every link pings, and only some links carry a pressure detector.
func (s *Link) recordRTT(rtt time.Duration, now time.Time) {
	srtt := time.Duration(s.srtt.Load())
	if srtt == 0 {
		srtt = rtt
	} else {
		srtt += (rtt - srtt) / srttWeight
	}
	s.srtt.Store(int64(srtt))
	// why relative to createdAt: the monotonic clock keeps RTTAge right across wall clock jumps.
	s.lastPong.Store(int64(now.Sub(s.createdAt)))
}

// Info describes the link as a mod.nodes.link_info.
func (s *Link) Info() *nodes.LinkInfo {
	age := time.Since(s.createdAt)
	info := &nodes.LinkInfo{
		ID:              s.id,
		LocalIdentity:   s.LocalIdentity(),
		RemoteIdentity:  s.RemoteIdentity(),
		LocalEndpoint:   knownEndpoint(s.LocalEndpoint()),
		RemoteEndpoint:  knownEndpoint(s.RemoteEndpoint()),
		Outbound:        astral.Bool(s.outbound),
		Network:         astral.String8(s.Network()),
		HighPressure:    astral.Bool(s.PressureHigh()),
		BytesThroughput: astral.Uint64(s.Throughput()),
		Age:             astral.Duration(age),
	}
	if lastPong := time.Duration(s.lastPong.Load()); lastPong != 0 {
		rtt := astral.Duration(s.srtt.Load())
		rttAge := astral.Duration(age - lastPong)
		info.RTT, info.RTTAge = &rtt, &rttAge
	}
	return info
}
