package nat

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/stretchr/testify/require"
)

func TestHole_Keepalive_Basic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	peerA, err := astral.ParseIdentity("0332af67ded5758128b3f795094eaf52522e17d7c4eb38dbd1b83b25cef2ed92ff")
	require.NoError(t, err)

	peerB, err := astral.ParseIdentity("0378814eb07439d54fa64e0ca78620c317265bb1993e35281404421481d6e0d722")
	require.NoError(t, err)

	aConn, bConn := NewPipePacketPair(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
	)

	localIP, err := ip.ParseIP("10.0.0.1")
	require.NoError(t, err)
	remoteIP, err := ip.ParseIP("10.0.0.2")
	require.NoError(t, err)

	holeA := NewHoleWithConn(
		nat.Hole{
			ActiveIdentity:  peerA,
			ActiveEndpoint:  nat.Endpoint{IP: localIP, Port: 40000},
			PassiveIdentity: peerB,
			PassiveEndpoint: nat.Endpoint{IP: remoteIP, Port: 40001},
		},
		peerA, true, aConn,
	)

	holeB := NewHoleWithConn(
		nat.Hole{
			ActiveIdentity:  peerB,
			ActiveEndpoint:  nat.Endpoint{IP: remoteIP, Port: 40001},
			PassiveIdentity: peerA,
			PassiveEndpoint: nat.Endpoint{IP: localIP, Port: 40000},
		},
		peerB, false, bConn,
	)

	startingPong := holeA.LastPing()
	require.NoError(t, holeA.StartKeepAlive(ctx))
	require.NoError(t, holeB.StartKeepAlive(ctx))

	time.Sleep(5 * time.Second)
	require.NotEqual(t, startingPong.Unix(), holeA.LastPing().Unix(), "holeA.LastPing() should have changed")
	require.NotEqual(t, startingPong.Unix(), holeB.LastPing().Unix(), "holeB.LastPing() should have changed")

	require.Equal(t, StateIdle, holeA.State())
	require.Equal(t, StateIdle, holeB.State())
}

func TestHole_NoPong(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	peerA, err := astral.ParseIdentity("0332af67ded5758128b3f795094eaf52522e17d7c4eb38dbd1b83b25cef2ed92ff")
	require.NoError(t, err)

	remoteID, err := astral.ParseIdentity("0378814eb07439d54fa64e0ca78620c317265bb1993e35281404421481d6e0d722")
	require.NoError(t, err)

	aConn, _ := NewPipePacketPair(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
	)

	localIP, err := ip.ParseIP("10.0.0.1")
	require.NoError(t, err)
	remoteIP, err := ip.ParseIP("10.0.0.2")
	require.NoError(t, err)

	holeA := NewHoleWithConn(
		nat.Hole{
			ActiveIdentity:  peerA,
			ActiveEndpoint:  nat.Endpoint{IP: localIP, Port: 40000},
			PassiveIdentity: remoteID,
			PassiveEndpoint: nat.Endpoint{IP: remoteIP, Port: 40001},
		},
		peerA, true, aConn,
	)

	startingPong := holeA.LastPing()
	require.NoError(t, holeA.StartKeepAlive(ctx))

	// Allow some pings to be sent with no pongs returning
	time.Sleep(500 * time.Millisecond)

	require.Equal(t,
		startingPong.Unix(),
		holeA.LastPing().Unix(),
		"LastPing should NOT change when no pong arrives",
	)
}

func TestHole_LockCausesRemoteExpiration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	peerA, err := astral.ParseIdentity("0332af67ded5758128b3f795094eaf52522e17d7c4eb38dbd1b83b25cef2ed92ff")
	require.NoError(t, err)

	peerB, err := astral.ParseIdentity("0378814eb07439d54fa64e0ca78620c317265bb1993e35281404421481d6e0d722")
	require.NoError(t, err)

	aConn, bConn := NewPipePacketPair(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 40001},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 40000},
	)

	localIP, err := ip.ParseIP("10.0.0.1")
	require.NoError(t, err)
	remoteIP, err := ip.ParseIP("10.0.0.2")
	require.NoError(t, err)

	holeA := NewHoleWithConn(
		nat.Hole{
			ActiveIdentity:  peerA,
			ActiveEndpoint:  nat.Endpoint{IP: localIP, Port: 40000},
			PassiveIdentity: peerB,
			PassiveEndpoint: nat.Endpoint{IP: remoteIP, Port: 40001},
		},
		peerA, true, aConn,
	)

	holeB := NewHoleWithConn(
		nat.Hole{
			ActiveIdentity:  peerB,
			ActiveEndpoint:  nat.Endpoint{IP: remoteIP, Port: 40001},
			PassiveIdentity: peerA,
			PassiveEndpoint: nat.Endpoint{IP: localIP, Port: 40000},
		},
		peerB, false, bConn,
	)

	require.NoError(t, holeA.StartKeepAlive(ctx))
	require.NoError(t, holeB.StartKeepAlive(ctx))

	// Let keepalive stabilize
	time.Sleep(2 * time.Second)
	require.Equal(t, StateIdle, holeA.State())
	require.Equal(t, StateIdle, holeB.State())

	require.True(t, holeA.BeginLock())
	require.NoError(t, holeA.WaitLocked(ctx))
	require.Equal(t, StateLocked, holeA.State())

	// After locking, holeA stops sending pings.
	// holeB expires after noPingTimeout (15s) of silence.
	time.Sleep(16 * time.Second)

	require.Equal(t, StateExpired, holeB.State())
	require.Equal(t, StateLocked, holeA.State())
}

// freeUDPPort returns a UDP port nothing is bound to right now.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
	require.NoError(t, err)
	port := c.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, c.Close())
	return port
}

// Behind a NAT that translates ports the hole's external port is not a local
// one; NewHole binds the bind port the punch socket held.
func TestNewHole_BindsTheBindPortNotTheExternalPort(t *testing.T) {
	self, peer := astral.GenerateIdentity(), astral.GenerateIdentity()
	bindPort, externalPort := freeUDPPort(t), freeUDPPort(t)

	h, err := NewHole(nat.Hole{
		ActiveIdentity:  self,
		ActiveEndpoint:  nat.Endpoint{IP: ip.IP(net.ParseIP("212.105.131.237").To4()), Port: astral.Uint16(externalPort)},
		ActiveBindPort:  astral.Uint16(bindPort),
		PassiveIdentity: peer,
		PassiveEndpoint: nat.Endpoint{IP: ip.IP(net.ParseIP("45.59.119.13").To4()), Port: 55001},
		PassiveBindPort: 55001,
	}, self, true)
	require.NoError(t, err)
	defer h.conn.Close()

	require.Equal(t, bindPort, h.conn.LocalAddr().(*net.UDPAddr).Port)
	require.Equal(t, "45.59.119.13:55001", h.GetRemoteAddr(self).String())
}
