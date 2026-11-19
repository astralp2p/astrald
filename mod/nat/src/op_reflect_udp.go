package nat

import (
	"bytes"
	crand "crypto/rand"
	"net"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

const reflectTokenSize = 16

type opReflectUDPArgs struct {
	Out string
}

// OpReflectUDP tells the caller the source endpoint of one UDP datagram carrying
// a fresh token, so the caller learns how its NAT maps a punch socket.
//
// why no UDP reply: the observed endpoint goes back over the query channel, so
// a forged source address gains nothing to amplify.
func (mod *Module) OpReflectUDP(ctx *astral.Context, q *routing.IncomingQuery, args opReflectUDPArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if !mod.enabled.Load() {
		return ch.Send(nat.ErrDisabled)
	}

	conn, err := mod.listenReflector()
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	defer conn.Close()

	seen, err := reflectOnce(conn, ch)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "reflected %v to %v", seen, q.Caller())
	return ch.Send(seen)
}

// listenReflector binds the socket of one reflection.
func (mod *Module) listenReflector() (net.PacketConn, error) {
	if mod.listenUDP != nil {
		return mod.listenUDP()
	}
	return net.ListenPacket("udp4", ":0")
}

// reflectOnce offers conn's port and a fresh token on ch, then returns the
// source of the first datagram carrying the token within nat.ReflectTimeout.
func reflectOnce(conn net.PacketConn, ch *channel.Channel) (*nat.Endpoint, error) {
	token := make([]byte, reflectTokenSize)
	if _, err := crand.Read(token); err != nil {
		return nil, err
	}

	port := 0
	if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		port = ua.Port
	}
	if err := ch.Send(&nat.ReflectUDPOffer{Port: astral.Uint16(port), Token: token}); err != nil {
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(nat.ReflectTimeout))
	buf := make([]byte, 64)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return nil, nat.ErrNoReflection
		}
		ua, ok := addr.(*net.UDPAddr)
		if !ok || !bytes.Equal(buf[:n], token) {
			continue
		}
		// note: To4 keeps an IPv4 source 4 bytes long, as the punch signals carry it.
		seenIP := ua.IP
		if v4 := ua.IP.To4(); v4 != nil {
			seenIP = v4
		}
		return &nat.Endpoint{IP: ip.IP(seenIP), Port: astral.Uint16(ua.Port)}, nil
	}
}
