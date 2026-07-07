package ip

import (
	"fmt"
	ipmod "github.com/cryptopunkscc/astrald/mod/ip"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/ip"
)

func (mod *Module) DefaultGateway() (ip.IP, error) {
	switch runtime.GOOS {
	case "linux":
		return mod.defaultGatewayLinux()
	case "darwin": // macOS
		return mod.defaultGatewayDarwin()
	case "windows":
		return mod.defaultGatewayWindows()
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func (mod *Module) defaultGatewayLinux() (ip.IP, error) {
	// Run `ip route` command to get routing table
	cmd := exec.Command("ip", "route")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run ip route: %v", err)
	}

	// Parse output to find default gateway
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "default via") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				ipaddr := fields[2]
				if parsed, _ := ip.ParseIP(ipaddr); parsed != nil {
					return parsed, nil
				}
			}
		}
	}
	return nil, ipmod.ErrDefaultGatewayNotFound
}

func (mod *Module) defaultGatewayDarwin() (ip.IP, error) {
	// Run `netstat -nr` command to get routing table
	cmd := exec.Command("netstat", "-nr")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run netstat: %v", err)
	}

	// Parse output to find default gateway
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "default") && strings.Contains(line, "UG") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				ipAddr := fields[1]
				if parsed, _ := ip.ParseIP(ipAddr); parsed != nil {
					return parsed, nil
				}
			}
		}
	}
	return nil, ipmod.ErrDefaultGatewayNotFound
}

func (mod *Module) defaultGatewayWindows() (ip.IP, error) {
	// Run `netsh interface ip show config` command
	cmd := exec.Command("netsh", "interface", "ip", "show", "config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run netsh: %v", err)
	}

	// Parse output to find default gateway
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Default Gateway") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				ipAddr := strings.TrimSpace(parts[1])
				if parsed, _ := ip.ParseIP(ipAddr); parsed != nil {
					return parsed, nil
				}
			}
		}
	}
	return nil, ipmod.ErrDefaultGatewayNotFound
}
