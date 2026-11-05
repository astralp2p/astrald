package ether

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

// linkTTL is the IP TTL of every ether datagram.
// why: a router decrements the TTL, so a datagram that arrives with 255 was sent on the link itself (RFC 5082).
const linkTTL = 255

// setupSocket binds the IPv4 UDP socket that sends and receives ether datagrams.
func (mod *Module) setupSocket() error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: mod.config.UDPPort})
	if err != nil {
		return err
	}

	socket := ipv4.NewPacketConn(conn)
	if err := configureSocket(socket); err != nil {
		socket.Close()
		return err
	}

	mod.socket = socket

	return nil
}

// configureSocket sets the send TTL and, where the platform reports it, the TTL of each received datagram.
func configureSocket(socket *ipv4.PacketConn) error {
	if err := socket.SetTTL(linkTTL); err != nil {
		return fmt.Errorf("set ttl: %w", err)
	}

	if !checkInboundTTL {
		return nil
	}

	// why: without the TTL control message every received datagram fails the TTL check.
	if err := socket.SetControlMessage(ipv4.FlagTTL, true); err != nil {
		return fmt.Errorf("enable ttl control messages: %w", err)
	}

	return nil
}
