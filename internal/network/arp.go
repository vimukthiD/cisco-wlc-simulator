package network

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
)

// probeAvailable is set to false if ARP raw socket fails (no root).
var probeAvailable = true

// ProbeIP sends an ARP request to check if an IP is in use on the given interface.
// Returns true if a response is received (IP is taken).
// If raw sockets aren't available (no root), returns false (optimistic).
func ProbeIP(ifaceName string, ip net.IP) bool {
	if !probeAvailable {
		return false // can't probe, assume available
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return true
	}

	client, err := arp.Dial(iface)
	if err != nil {
		probeAvailable = false
		return false
	}
	defer client.Close()

	deadline := time.Now().Add(800 * time.Millisecond)
	client.SetDeadline(deadline)

	addr, ok := netip.AddrFromSlice(ip.To4())
	if !ok {
		return true
	}
	if err := client.Request(addr); err != nil {
		return false
	}

	// Read packets until deadline, looking for a reply matching our probe.
	// The raw socket receives ALL ARP traffic, so we must filter.
	for time.Now().Before(deadline) {
		pkt, _, err := client.Read()
		if err != nil {
			return false // timeout = no reply = IP is free
		}
		// Check if this is a reply for the IP we probed
		if pkt.Operation == arp.OperationReply && pkt.SenderIP == addr {
			log.Printf("    ARP: %s is taken (reply from %s)", ip, pkt.SenderHardwareAddr)
			return true // someone owns this IP
		}
	}
	return false // no matching reply = IP is free
}

// FindUnusedIPs finds count unused IPs in the given subnet.
// Uses ARP probing when available (root), otherwise falls back to
// checking the system ARP cache and local interfaces.
func FindUnusedIPs(ifaceName string, subnet *net.IPNet, count int) ([]net.IP, error) {
	var result []net.IP

	ip := make(net.IP, len(subnet.IP))
	copy(ip, subnet.IP)
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("not an IPv4 subnet")
	}

	// Build a set of known-used IPs from ARP cache and local interfaces
	knownUsed := getKnownUsedIPs()
	log.Printf("    Known used IPs: %d entries", len(knownUsed))

	isUsed := func(candidate net.IP) bool {
		if knownUsed[candidate.String()] {
			return true
		}
		return ProbeIP(ifaceName, candidate)
	}

	// Start from .200 to avoid DHCP pools (typically .100-.199) and low ranges
	for c := 200; c < 255 && len(result) < count; c++ {
		candidate := net.IPv4(ip[0], ip[1], ip[2], byte(c))
		if !subnet.Contains(candidate) {
			continue
		}
		if !isUsed(candidate) {
			result = append(result, candidate)
		}
	}

	// Try .150-.199
	for c := 150; c < 200 && len(result) < count; c++ {
		candidate := net.IPv4(ip[0], ip[1], ip[2], byte(c))
		if !subnet.Contains(candidate) {
			continue
		}
		if !isUsed(candidate) {
			result = append(result, candidate)
		}
	}

	if len(result) < count {
		return result, fmt.Errorf("only found %d unused IPs (needed %d)", len(result), count)
	}
	return result, nil
}

// getKnownUsedIPs returns a set of IPs that are known to be in use by
// reading the system ARP cache and local interface addresses (no root needed).
func getKnownUsedIPs() map[string]bool {
	used := make(map[string]bool)

	// 1. Local interface addresses
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ip4 := ipNet.IP.To4(); ip4 != nil {
					used[ip4.String()] = true
				}
			}
		}
	}

	// 2. System ARP cache
	for _, ip := range readARPCache() {
		used[ip] = true
	}

	// 3. Common reserved addresses
	// .1 is typically the gateway
	used["0.0.0.0"] = true

	return used
}

// readARPCache reads the system ARP table without needing root.
func readARPCache() []string {
	var ips []string

	switch runtime.GOOS {
	case "darwin":
		// arp -a output: "? (192.168.1.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]"
		// Skip "(incomplete)" entries — those are stale/failed lookups, not real devices
		out, err := exec.Command("arp", "-a").Output()
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "(incomplete)") {
				continue
			}
			if start := strings.Index(line, "("); start >= 0 {
				if end := strings.Index(line[start:], ")"); end >= 0 {
					ips = append(ips, line[start+1:start+end])
				}
			}
		}
	case "linux":
		// ip neigh output: "192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE"
		out, err := exec.Command("ip", "neigh").Output()
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				if net.ParseIP(fields[0]) != nil {
					ips = append(ips, fields[0])
				}
			}
		}
	}
	return ips
}

// AnnounceIP sends gratuitous ARP announcements so other hosts on the
// network learn the MAC address for our new IP. Sends multiple packets
// using both the Go library and the system arping command as fallback.
func AnnounceIP(ifaceName string, ip net.IP) error {
	ipStr := ip.String()

	// Method 1: Use system arping/arp command (most reliable on macOS)
	switch runtime.GOOS {
	case "darwin":
		// On macOS, send a few pings from the new IP to populate ARP caches.
		// Also use arp -s to create a local entry.
		exec.Command("ping", "-c", "1", "-S", ipStr, "-t", "1", "255.255.255.255").Run()
		exec.Command("ping", "-c", "1", "-S", ipStr, "-t", "1",
			ipStr[:strings.LastIndex(ipStr, ".")]+".255").Run()
	case "linux":
		// arping sends proper gratuitous ARP
		exec.Command("arping", "-A", "-c", "3", "-I", ifaceName, ipStr).Run()
	}

	// Method 2: Use the Go ARP library
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil // best-effort
	}
	client, err := arp.Dial(iface)
	if err != nil {
		return nil // best-effort
	}
	defer client.Close()

	addr, ok := netip.AddrFromSlice(ip.To4())
	if !ok {
		return nil
	}

	// Send gratuitous ARP reply (broadcast) — 3 times for reliability
	for i := 0; i < 3; i++ {
		pkt, err := arp.NewPacket(
			arp.OperationReply,
			iface.HardwareAddr,
			addr,
			ethernet.Broadcast,
			addr,
		)
		if err != nil {
			continue
		}
		client.WriteTo(pkt, ethernet.Broadcast)
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("    ARP announced %s on %s (%s)", ipStr, ifaceName, iface.HardwareAddr)
	return nil
}
