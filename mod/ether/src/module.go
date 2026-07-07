package ether

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/ether"
	"github.com/cryptopunkscc/astrald/mod/ip"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
	"github.com/cryptopunkscc/astrald/resources"
)

const maxBroadcastSize = 1<<16 - 1

var _ ether.Module = &Module{}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	socket *net.UDPConn

	ctx *astral.Context
}

// Run starts the broadcast receiver loop and holds a platform multicast lock for the lifetime of ctx.
// When socket is nil (UDP bind failed at load), Run exits cleanly without any network activity.
func (mod *Module) Run(ctx *astral.Context) (err error) {
	mod.ctx = ctx

	// If the socket failed to bind during Load, run as a no-op: no broadcasts
	// in or out, no platform multicast lock requested.
	if mod.socket == nil {
		<-ctx.Done()
		return nil
	}

	if hook := ether.LANDiscoveryHook; hook != nil {
		hook(true)
		defer hook(false)
	}

	go mod.broadcastReceiver(ctx)

	<-ctx.Done()

	mod.socket.Close()

	return nil
}

func (mod *Module) broadcastReceiver(ctx *astral.Context) (err error) {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		b, addr, err := mod.readBroadcast()
		if err != nil {
			mod.log.Errorv(2, "read broadcast: %v", err)
			continue
		}

		mod.Objects.Receive(&ether.EventBroadcastReceived{
			SourceIP: ip.IP(addr.IP),
			Object:   b.Object,
		}, mod.node.Identity())

		err = mod.Objects.Receive(b.Object, b.Source)
		if err == nil {
			objectID, err := astral.ResolveObjectID(b.Object)
			if err == nil {
				mod.log.Logv(2, "received from %v object %v (%v)", b.Source, b.Object.ObjectType(), objectID)
			}
		}
	}
}

// Push pushes an object to everyone (broadcast)
func (mod *Module) Push(object astral.Object, source *astral.Identity) (err error) {
	packet, err := mod.makePacket(object, source)
	if err != nil {
		return err
	}

	return mod.broadcast(packet)
}

// PushToIP pushes an object to a specific IP address (unicast)
func (mod *Module) PushToIP(ip ip.IP, object astral.Object, source *astral.Identity) error {
	packet, err := mod.makePacket(object, source)
	if err != nil {
		return err
	}

	_, err = mod.writeToIP(ip, packet)
	return err
}

// readBroadcast reads the next broadcast from the UDP socket
func (mod *Module) readBroadcast() (*ether.SignedBroadcast, *net.UDPAddr, error) {
	if mod.socket == nil {
		return nil, nil, errors.New("socket not initialized")
	}

	for {
		buf := make([]byte, maxBroadcastSize)

		n, srcAddr, err := mod.socket.ReadFromUDP(buf)
		if err != nil {
			return nil, nil, err
		}

		var r = bytes.NewReader(buf[:n])

		var signed ether.SignedBroadcast
		_, err = signed.ReadFrom(r)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid broadcast object from %s: %w", srcAddr, err)
		}

		// ignore our own broadcasts
		if signed.Source.IsEqual(mod.node.Identity()) {
			continue
		}

		// check if the signature is present
		if signed.Signature == nil {
			continue
		}

		// verify signature
		err = mod.Crypto.VerifyHashSignature(secp256k1.FromIdentity(signed.Source), signed.Signature, signed.Hash())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid broadcast signature from %s", srcAddr)
		}

		return &signed, srcAddr, nil
	}
}

func (mod *Module) broadcast(data []byte) error {
	ifaces, err := NetInterfaces()
	if err != nil {
		return err
	}

	var broadcastedAddrs = sig.Set[string]{}
	// go over all network interfaces
	for _, iface := range ifaces {
		if !isInterfaceEnabled(iface) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return err
		}

		// go over all addresses of the interface
		for _, addr := range addrs {
			broadcastIP, err := BroadcastAddr(addr)
			if err != nil {
				return err
			}
			if broadcastedAddrs.Contains(broadcastIP.String()) {
				// already broadcasted this address from another interface
				continue
			}

			if IsLinkLocal(broadcastIP) {
				continue
			}

			_, err = mod.writeToIP(ip.IP(broadcastIP), data)
			if err != nil {
				return err
			}

			broadcastedAddrs.Add(broadcastIP.String())
		}
	}

	return nil
}

func (mod *Module) writeToIP(ip ip.IP, data []byte) (n int, err error) {
	if mod.socket == nil {
		return 0, errors.New("socket not initialized")
	}

	return mod.socket.WriteTo(data, &net.UDPAddr{
		IP:   net.IP(ip),
		Port: mod.config.UDPPort,
	})
}

func (mod *Module) makePacket(object astral.Object, source *astral.Identity) (data []byte, err error) {
	if source == nil {
		source = mod.node.Identity()
	}

	signed := &ether.SignedBroadcast{
		Broadcast: ether.Broadcast{
			Object:    object,
			Timestamp: astral.Time(time.Now()),
			Source:    source,
		},
	}

	var hash = signed.Hash()

	signed.Signature, err = mod.Crypto.NodeSigner().SignHash(mod.ctx, hash)
	if err != nil {
		return
	}

	packet := &bytes.Buffer{}
	_, err = signed.WriteTo(packet)
	if err != nil {
		return
	}

	return packet.Bytes(), nil
}

// setupSocket sets up the UDP socket for broadcasts. If ctx is not nil, the socket will close when the context gets canceled.
func (mod *Module) setupSocket() (err error) {
	// resolve local address
	var localAddr *net.UDPAddr
	localAddr, err = net.ResolveUDPAddr("udp", ":"+strconv.Itoa(mod.config.UDPPort))
	if err != nil {
		return
	}

	// bind the udp socket
	socket, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		mod.log.Errorv(2, "cannot listen for broadcasts: %v", err)
		return err
	}

	mod.socket = socket

	return
}

func isInterfaceEnabled(iface NetInterface) bool {
	return (iface.Flags&net.FlagUp != 0) &&
		(iface.Flags&net.FlagBroadcast != 0) &&
		(iface.Flags&net.FlagLoopback == 0)
}

// BroadcastAddr derives the IPv4 broadcast address from a CIDR net.Addr.
// note: slices IPv4-in-IPv6 addresses down to 4 bytes before applying the mask.
func BroadcastAddr(addr net.Addr) (net.IP, error) {
	ip, ipnet, err := net.ParseCIDR(addr.String())
	if err != nil {
		return nil, err
	}

	if len(ipnet.Mask) == net.IPv4len {
		ip = ip[12:]
	}

	broadIP := make(net.IP, len(ipnet.Mask))

	for i := 0; i < len(ipnet.Mask); i++ {
		broadIP[i] = ip[i] | ^ipnet.Mask[i]
	}

	return broadIP, nil
}

func IsLinkLocal(ip net.IP) bool {
	if ip := ip.To4(); ip != nil {
		return ip[0] == 169 && ip[1] == 254
	}
	return ip[0] == 0xfe && ip[1] == 0x80
}
