package network

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"

	"github.com/vimukthi/cisco-wlc-sim/internal/device"
)

// SetupIPs adds virtual IP aliases for each device.
// On macOS: ifconfig lo0 alias <ip>
// On Linux: ip addr add <ip>/32 dev lo
func SetupIPs(devices []device.Device) error {
	for _, dev := range devices {
		if isLocalIP(dev.IP) {
			log.Printf("  %s (%s) already exists, skipping", dev.IP, dev.Hostname)
			continue
		}
		if err := AddIP(dev.IP); err != nil {
			return fmt.Errorf("add IP %s: %w", dev.IP, err)
		}
		log.Printf("  Added %s (%s)", dev.IP, dev.Hostname)
	}
	return nil
}

// TeardownIPs removes virtual IP aliases for each device.
func TeardownIPs(devices []device.Device) {
	for _, dev := range devices {
		if err := RemoveIP(dev.IP); err != nil {
			log.Printf("  Warning: failed to remove %s: %v", dev.IP, err)
		} else {
			log.Printf("  Removed %s (%s)", dev.IP, dev.Hostname)
		}
	}
}

// AddIP adds a single virtual IP alias to the loopback interface.
func AddIP(ip string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("ifconfig", "lo0", "alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "add", ip+"/32", "dev", "lo").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// RemoveIP removes a single virtual IP alias from the loopback interface.
func RemoveIP(ip string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("ifconfig", "lo0", "-alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "del", ip+"/32", "dev", "lo").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// AddIPToInterface adds an IP alias to a specific network interface.
// On macOS: uses plain alias (inherits subnet from primary IP).
// On Linux: uses the provided CIDR prefix.
func AddIPToInterface(iface, ip string, prefixLen int) error {
	if prefixLen == 0 {
		prefixLen = 24
	}
	switch runtime.GOOS {
	case "darwin":
		// Plain alias — macOS inherits routing from the primary address
		return exec.Command("ifconfig", iface, "alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "add", fmt.Sprintf("%s/%d", ip, prefixLen), "dev", iface).Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// RemoveIPFromInterface removes an IP alias from a specific network interface.
func RemoveIPFromInterface(iface, ip string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("ifconfig", iface, "-alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "del", ip+"/32", "dev", iface).Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// SetupLANIPs adds IPs to a physical interface for LAN accessibility.
// Device IPs that are not in the detected LAN subnet are replaced with
// auto-assigned IPs from the subnet. Sends gratuitous ARP after each add.
func SetupLANIPs(devices []device.Device, ifaceName string) (*InterfaceInfo, error) {
	info, err := DetectPrimaryInterface(ifaceName)
	if err != nil {
		return nil, err
	}

	log.Printf("  LAN mode: using %s (%s)", info.Name, info.Subnet.String())

	// Count how many devices need auto-assigned IPs:
	// any device with no IP, "auto", or an IP outside the LAN subnet
	needIPs := 0
	for _, dev := range devices {
		if needsAutoIP(dev.IP, info.Subnet) {
			needIPs++
		}
	}

	var autoIPs []net.IP
	if needIPs > 0 {
		autoIPs, err = FindUnusedIPs(info.Name, info.Subnet, needIPs)
		if err != nil {
			return nil, fmt.Errorf("find unused IPs: %w", err)
		}
	}

	autoIdx := 0
	for i := range devices {
		dev := &devices[i]
		if needsAutoIP(dev.IP, info.Subnet) {
			oldIP := dev.IP
			dev.IP = autoIPs[autoIdx].String()
			autoIdx++
			if oldIP != "" && oldIP != "auto" {
				log.Printf("  %s (%s) reassigned from %s (outside LAN subnet)", dev.IP, dev.Hostname, oldIP)
			} else {
				log.Printf("  %s (%s) auto-assigned", dev.IP, dev.Hostname)
			}
		}

		if isLocalIP(dev.IP) {
			log.Printf("  %s (%s) already exists, skipping", dev.IP, dev.Hostname)
			continue
		}

		ones, _ := info.Subnet.Mask.Size()
		if err := AddIPToInterface(info.Name, dev.IP, ones); err != nil {
			return nil, fmt.Errorf("add IP %s to %s: %w", dev.IP, info.Name, err)
		}

		// On macOS, also add to loopback so the host can reach its own aliases
		if runtime.GOOS == "darwin" {
			exec.Command("ifconfig", "lo0", "alias", dev.IP).Run()
		}

		if err := AnnounceIP(info.Name, net.ParseIP(dev.IP)); err != nil {
			log.Printf("  Warning: gratuitous ARP for %s failed: %v", dev.IP, err)
		}

		log.Printf("  Added %s (%s) on %s (+ lo0)", dev.IP, dev.Hostname, info.Name)
	}
	return info, nil
}

// TeardownLANIPs removes IPs from a physical interface (and loopback on macOS).
func TeardownLANIPs(devices []device.Device, ifaceName string) {
	for _, dev := range devices {
		if err := RemoveIPFromInterface(ifaceName, dev.IP); err != nil {
			log.Printf("  Warning: failed to remove %s from %s: %v", dev.IP, ifaceName, err)
		}
		if runtime.GOOS == "darwin" {
			exec.Command("ifconfig", "lo0", "-alias", dev.IP).Run()
		}
		log.Printf("  Removed %s (%s)", dev.IP, dev.Hostname)
	}
}

// needsAutoIP returns true if the device IP should be replaced with
// an auto-assigned LAN IP — either it's empty/auto, or it's outside
// the detected LAN subnet.
func needsAutoIP(ip string, subnet *net.IPNet) bool {
	if ip == "" || ip == "auto" {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	return !subnet.Contains(parsed)
}

func isLocalIP(ip string) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var addrIP net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			addrIP = v.IP
		case *net.IPAddr:
			addrIP = v.IP
		}
		if addrIP != nil && addrIP.String() == ip {
			return true
		}
	}
	return false
}
