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
		if err := addIP(dev.IP); err != nil {
			return fmt.Errorf("add IP %s: %w", dev.IP, err)
		}
		log.Printf("  Added %s (%s)", dev.IP, dev.Hostname)
	}
	return nil
}

// TeardownIPs removes virtual IP aliases for each device.
func TeardownIPs(devices []device.Device) {
	for _, dev := range devices {
		if err := removeIP(dev.IP); err != nil {
			log.Printf("  Warning: failed to remove %s: %v", dev.IP, err)
		} else {
			log.Printf("  Removed %s (%s)", dev.IP, dev.Hostname)
		}
	}
}

func addIP(ip string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("ifconfig", "lo0", "alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "add", ip+"/32", "dev", "lo").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func removeIP(ip string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("ifconfig", "lo0", "-alias", ip).Run()
	case "linux":
		return exec.Command("ip", "addr", "del", ip+"/32", "dev", "lo").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
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
