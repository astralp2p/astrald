package ether

import (
	"errors"
	"fmt"
	"net"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	ethermod "github.com/astralp2p/astrald/mod/ether"
	"github.com/astralp2p/astrald/resources"
	"golang.org/x/net/ipv4"
)

var _ ethermod.Module = &Module{}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	socket *ipv4.PacketConn

	// note: tests replace lookupPrefixes to inject the local prefixes.
	lookupPrefixes prefixLookup
}

// Run starts the broadcast receiver loop and holds a platform multicast lock for the lifetime of ctx.
// When socket is nil (listen is off or the UDP bind failed at load), Run exits cleanly without any network activity.
func (mod *Module) Run(ctx *astral.Context) (err error) {
	// If listen is off or the socket failed to bind during Load, run as a no-op: no broadcasts
	// in or out, no platform multicast lock requested.
	if mod.socket == nil {
		<-ctx.Done()
		return nil
	}

	if hook := ethermod.LANDiscoveryHook; hook != nil {
		hook(true)
		defer hook(false)
	}

	go mod.receiveLoop(ctx)

	<-ctx.Done()

	mod.socket.Close()

	return nil
}

// Close releases the socket Load bound. The module manager calls it when a
// load error stops the node before Run, which otherwise closes the socket.
func (mod *Module) Close() error {
	if mod.socket == nil {
		return nil
	}
	return mod.socket.Close()
}

// Push pushes an object to everyone (broadcast)
func (mod *Module) Push(object astral.Object) (err error) {
	packet, err := makePacket(object)
	if err != nil {
		return err
	}

	return broadcast(packet, mod.writeToIP)
}

// PushToSubnetOf broadcasts an object on the local IPv4 subnet that contains src only.
func (mod *Module) PushToSubnetOf(src ip.IP, object astral.Object) error {
	return mod.pushToSubnetOf(src, object, mod.writeToIP)
}

func (mod *Module) pushToSubnetOf(src ip.IP, object astral.Object, send sendFunc) error {
	subnet, err := mod.findLocalSubnet(src)
	if err != nil {
		return err
	}

	packet, err := makePacket(object)
	if err != nil {
		return err
	}

	target, err := BroadcastAddr(subnet)
	if err != nil {
		return err
	}

	_, err = send(ip.IP(target), packet)
	return err
}

// SubnetOf returns the CIDR of the local IPv4 subnet that contains src.
func (mod *Module) SubnetOf(src ip.IP) (subnet string, ok bool) {
	found, err := mod.findLocalSubnet(src)
	if err != nil {
		return "", false
	}

	return found.String(), true
}

// findLocalSubnet returns the local IPv4 subnet that contains src, or ErrNotOnLink.
func (mod *Module) findLocalSubnet(src ip.IP) (*net.IPNet, error) {
	src4 := net.IP(src).To4()
	if src4 == nil {
		return nil, ethermod.ErrNotOnLink
	}

	local, err := mod.lookupPrefixes(src4)
	if err != nil {
		return nil, fmt.Errorf("find local prefixes: %w", err)
	}

	subnet, ok := findSubnet(local, src4)
	if !ok {
		return nil, ethermod.ErrNotOnLink
	}

	return subnet, nil
}

// note: sendFunc is the seam between broadcast and the UDP socket.
type sendFunc func(ip.IP, []byte) (int, error)

// why: one failing interface or send never stops the remaining targets.
// why: the joined error still reaches the callers, which log it (nearby status push) or return it (nearby broadcast op).
func broadcast(data []byte, send sendFunc) error {
	ifaces, err := NetInterfaces()
	if err != nil {
		return err
	}

	var errs []error
	var broadcastedAddrs = sig.Set[string]{}
	// go over all network interfaces
	for _, iface := range ifaces {
		if !isInterfaceEnabled(iface) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// go over all addresses of the interface
		for _, addr := range addrs {
			broadcastIP, err := BroadcastAddr(addr)
			if errors.Is(err, ErrNotIPv4) {
				// note: ether broadcasts are IPv4-only, so IPv6 prefixes are skipped
				continue
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if broadcastedAddrs.Contains(broadcastIP.String()) {
				// already broadcasted this address from another interface
				continue
			}

			if IsLinkLocal(broadcastIP) {
				continue
			}

			// why: the socket is not bound to an interface, so a failed address is not retried from another interface.
			broadcastedAddrs.Add(broadcastIP.String())

			_, err = send(ip.IP(broadcastIP), data)
			if err != nil {
				errs = append(errs, fmt.Errorf("send to %v: %w", broadcastIP, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (mod *Module) writeToIP(ip ip.IP, data []byte) (n int, err error) {
	if mod.socket == nil {
		return 0, errors.New("socket not initialized")
	}

	return mod.socket.WriteTo(data, nil, &net.UDPAddr{
		IP:   net.IP(ip),
		Port: mod.config.UDPPort,
	})
}

func isInterfaceEnabled(iface NetInterface) bool {
	return (iface.Flags&net.FlagUp != 0) &&
		(iface.Flags&net.FlagBroadcast != 0) &&
		(iface.Flags&net.FlagLoopback == 0)
}

// ErrNotIPv4 is returned by BroadcastAddr for prefixes that have no IPv4 broadcast address.
var ErrNotIPv4 = errors.New("not an IPv4 prefix")

// BroadcastAddr derives the IPv4 broadcast address from a CIDR net.Addr.
// note: slices IPv4-in-IPv6 addresses down to 4 bytes before applying the mask.
// IPv6 prefixes have no broadcast address and yield ErrNotIPv4.
func BroadcastAddr(addr net.Addr) (net.IP, error) {
	ip, ipnet, err := net.ParseCIDR(addr.String())
	if err != nil {
		return nil, err
	}

	if len(ipnet.Mask) != net.IPv4len {
		return nil, ErrNotIPv4
	}
	ip = ip[12:]

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
	return len(ip) == net.IPv6len && ip[0] == 0xfe && ip[1]&0xc0 == 0x80
}
