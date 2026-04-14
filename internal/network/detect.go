package network

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// InterfaceInfo holds details about the primary network interface.
type InterfaceInfo struct {
	Name   string
	IP     net.IP
	Subnet *net.IPNet
	HWAddr net.HardwareAddr
}

// DetectPrimaryInterface finds the default-route interface and its IP/subnet.
// If ifaceName is non-empty, it uses that interface instead of auto-detecting.
func DetectPrimaryInterface(ifaceName string) (*InterfaceInfo, error) {
	if ifaceName == "" {
		// Retry auto-detection for up to 60s to handle boot races where DHCP
		// hasn't installed a default route yet (VM appliance use case).
		var err error
		deadline := time.Now().Add(60 * time.Second)
		for {
			ifaceName, err = detectDefaultInterface()
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("detect default interface: %w", err)
			}
			log.Printf("  waiting for default route... (%v)", err)
			time.Sleep(2 * time.Second)
		}
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("interface %s addrs: %w", ifaceName, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil {
			continue
		}
		return &InterfaceInfo{
			Name:   ifaceName,
			IP:     ip4,
			Subnet: ipNet,
			HWAddr: iface.HardwareAddr,
		}, nil
	}

	return nil, fmt.Errorf("no IPv4 address found on interface %s", ifaceName)
}

func detectDefaultInterface() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return detectDefaultInterfaceDarwin()
	case "linux":
		return detectDefaultInterfaceLinux()
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func detectDefaultInterfaceDarwin() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", fmt.Errorf("route get default: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:")), nil
		}
	}
	return "", fmt.Errorf("could not find interface in route output")
}

func detectDefaultInterfaceLinux() (string, error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", fmt.Errorf("ip route show default: %w", err)
	}
	// Format: "default via 192.168.1.1 dev eth0 ..."
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no default route yet (output: %q)", strings.TrimSpace(string(out)))
}
