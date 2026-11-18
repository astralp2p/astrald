package ip

import (
	"net"
	"regexp"
	"strings"
)

// hostOnlyIfacePrefixes name interfaces of container and virtual-machine bridges. Their
// addresses are reachable only from the host itself, and default subnets repeat across
// hosts, so a remote peer dialing one reaches its own host instead.
//
// The names are Linux interface names. macOS vmnet bridges (bridge100 and up) and Windows
// Hyper-V switches ("vEthernet (...)") are not matched, so their addresses are still advertised.
var hostOnlyIfacePrefixes = []string{
	"docker", // docker0, docker_gwbridge
	"podman", // netavark bridges
	"cni",    // cni0, cni-podman0
	"virbr",  // libvirt
	"veth",
	"lxcbr",
	"lxdbr",
	"vboxnet",
	"vmnet",
}

// dockerNetworkIface matches the bridges Docker creates for user-defined networks.
//
// why: a bare "br-" prefix would also match real LAN bridges such as OpenWrt's br-lan.
var dockerNetworkIface = regexp.MustCompile(`^br-[0-9a-f]{12}$`)

func isHostOnlyIface(name string) bool {
	if dockerNetworkIface.MatchString(name) {
		return true
	}
	for _, p := range hostOnlyIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// hostOnlyIPs returns the addresses bound to host-only interfaces, keyed by string form.
// An interface listing failure yields none.
//
// why: Android blocks net.Interfaces; there are no container bridges to exclude there.
func hostOnlyIPs() map[string]bool {
	byIface, err := NetInterfaceAddrs()
	if err != nil {
		return nil
	}

	out := map[string]bool{}
	for name, addrs := range byIface {
		if !isHostOnlyIface(name) {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP != nil {
				out[ipnet.IP.String()] = true
			}
		}
	}
	return out
}
