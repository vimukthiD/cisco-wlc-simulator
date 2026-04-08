package sshsim

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pin/tftp/v3"
	"github.com/pkg/sftp"
	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/config"
	"github.com/vimukthi/cisco-wlc-sim/internal/device"
	"github.com/vimukthi/cisco-wlc-sim/internal/tftpsim"
	"golang.org/x/crypto/ssh"
)

// Serve starts an SSH server for the given device.
func Serve(dev *device.Device, auth config.Auth, logs *accesslog.Store, tftpMgr *tftpsim.Manager) error {
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == auth.Username && string(password) == auth.Password {
				return nil, nil
			}
			return nil, fmt.Errorf("authentication failed for %s", conn.User())
		},
	}

	hostKey, err := generateHostKey()
	if err != nil {
		return fmt.Errorf("generate host key: %w", err)
	}
	sshConfig.AddHostKey(hostKey)

	addr := fmt.Sprintf("%s:%d", dev.IP, dev.SSHPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	log.Printf("[%s] SSH listening on %s", dev.Hostname, addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[%s] SSH accept error: %v", dev.Hostname, err)
			continue
		}
		go handleConnection(conn, sshConfig, dev, logs, tftpMgr)
	}
}

func handleConnection(conn net.Conn, config *ssh.ServerConfig, dev *device.Device, logs *accesslog.Store, tftpMgr *tftpsim.Manager) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "ssh",
			Source:     remoteAddr,
			Detail:     "auth failed",
		})
		return
	}
	defer sshConn.Close()

	user := sshConn.User()
	logs.Add(accesslog.Entry{
		DeviceHost: dev.Hostname,
		DeviceIP:   dev.IP,
		Type:       "ssh",
		Source:     remoteAddr,
		User:       user,
		Detail:     "session opened",
	})

	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChan.Accept()
		if err != nil {
			continue
		}

		go handleSession(channel, requests, dev, logs, user, remoteAddr, tftpMgr)
	}
}

func handleSession(channel ssh.Channel, requests <-chan *ssh.Request, dev *device.Device, logs *accesslog.Store, user, remoteAddr string, tftpMgr *tftpsim.Manager) {
	defer channel.Close()

	logCmd := func(cmd string) {
		logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "ssh",
			Source:     remoteAddr,
			User:       user,
			Command:    cmd,
		})
	}

	// Handle session requests (pty, shell, exec)
	for req := range requests {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			runShell(channel, dev, logCmd, tftpMgr)
			return
		case "exec":
			req.Reply(true, nil)
			// Extract command from exec payload
			if len(req.Payload) > 4 {
				cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
				if cmdLen <= len(req.Payload)-4 {
					cmd := string(req.Payload[4 : 4+cmdLen])
					logCmd(cmd)
					if handleSCP(cmd, channel, dev) {
						return
					}
					output := executeCommand(cmd, dev, tftpMgr)
					channel.Write([]byte(output))
				}
			}
			return
		case "subsystem":
			// Extract subsystem name
			if len(req.Payload) > 4 {
				nameLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
				if nameLen <= len(req.Payload)-4 {
					subsystem := string(req.Payload[4 : 4+nameLen])
					if subsystem == "sftp" {
						req.Reply(true, nil)
						serveSFTP(channel, dev)
						return
					}
				}
			}
			req.Reply(false, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func runShell(channel ssh.Channel, dev *device.Device, logCmd func(string), tftpMgr *tftpsim.Manager) {
	prompt := dev.Hostname + "#"
	channel.Write([]byte("\r\n" + prompt))

	buf := make([]byte, 1024)
	var line []byte

	for {
		n, err := channel.Read(buf)
		if err != nil {
			return
		}

		for i := 0; i < n; i++ {
			ch := buf[i]
			switch ch {
			case '\r', '\n':
				channel.Write([]byte("\r\n"))
				cmd := strings.TrimSpace(string(line))
				if cmd == "exit" || cmd == "quit" || cmd == "logout" {
					channel.Write([]byte("Connection closed.\r\n"))
					return
				}
				if cmd != "" {
					logCmd(cmd)
					if handleInteractiveCopy(cmd, channel, dev, tftpMgr, logCmd) {
						// handled interactively
					} else {
						output := executeCommand(cmd, dev, tftpMgr)
						channel.Write([]byte(output))
					}
				}
				line = nil
				channel.Write([]byte(prompt))
			case 127, '\b': // backspace
				if len(line) > 0 {
					line = line[:len(line)-1]
					channel.Write([]byte("\b \b"))
				}
			case 3: // Ctrl+C
				line = nil
				channel.Write([]byte("^C\r\n" + prompt))
			default:
				line = append(line, ch)
				channel.Write([]byte{ch})
			}
		}
	}
}

func executeCommand(cmd string, dev *device.Device, tftpMgr *tftpsim.Manager) string {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}

	// Strip "admin" prefix (e.g., "admin show version" → "show version")
	if len(parts) > 1 && strings.ToLower(parts[0]) == "admin" {
		parts = parts[1:]
		cmd = strings.Join(parts, " ")
	}

	switch {
	case matchCmd(parts, "show", "version"):
		return showVersion(dev)
	case matchCmd(parts, "show", "wireless", "client", "summary"):
		return showWirelessClientSummary(dev)
	case matchCmd(parts, "show", "wireless", "client", "mac-address") && len(parts) >= 5:
		return showWirelessClientDetail(dev, parts[4])
	case matchCmd(parts, "show", "ip", "interface", "brief"):
		return showIPInterfaceBrief(dev)
	case matchCmd(parts, "show", "ap", "summary"):
		return showAPSummary(dev)
	case matchCmd(parts, "show", "wlan", "summary"):
		return showWLANSummary(dev)
	case matchCmd(parts, "show", "running-config"):
		return showRunningConfig(dev)
	case matchCmd(parts, "show", "startup-config"):
		return showStartupConfig(dev)
	case matchCmd(parts, "show", "sdwan", "running-config"):
		return showRunningConfig(dev)
	case matchCmd(parts, "show", "snmp"):
		return showSNMP(dev)
	case matchCmd(parts, "show", "inventory"):
		return showInventory(dev)
	case matchCmd(parts, "show", "diagbus"):
		return showDiag(dev)
	case matchCmd(parts, "show", "diag"):
		return showDiag(dev)
	case matchCmd(parts, "show", "install", "running"):
		return showInstallRunning(dev)
	case matchCmd(parts, "show", "interfaces"):
		return showInterfaces(dev)
	case matchCmd(parts, "show", "ip", "ospf", "interface"):
		return ""
	case matchCmd(parts, "show", "standby"):
		return ""
	case matchCmd(parts, "show", "vrrp"):
		return ""
	case matchCmd(parts, "show", "glbp"):
		return ""
	case matchCmd(parts, "show", "vrf"):
		return showVRF()
	case matchCmd(parts, "show", "vlan"):
		return showVLAN(dev)
	case matchCmd(parts, "show", "vtp", "status"):
		return showVTPStatus(dev)
	case matchCmd(parts, "show", "clock"):
		return showClock()
	case matchCmd(parts, "show", "ntp"):
		return showNTP()
	case matchCmd(parts, "dir"):
		return showDir(parts)
	case parts[0] == "enable":
		return ""
	case matchCmd(parts, "terminal", "length"):
		return ""
	case matchCmd(parts, "terminal", "width"):
		return ""
	case matchCmd(parts, "copy"):
		return "!!\r\n"
	case matchCmd(parts, "configure", "terminal"):
		return ""
	case parts[0] == "end":
		return ""
	case parts[0] == "?", cmd == "help":
		return showHelp()
	default:
		return fmt.Sprintf("%% Invalid input detected at '^' marker.\r\n")
	}
}

func matchCmd(parts []string, expected ...string) bool {
	if len(parts) < len(expected) {
		return false
	}
	for i, exp := range expected {
		if !strings.HasPrefix(exp, strings.ToLower(parts[i])) {
			return false
		}
	}
	return true
}

func showHelp() string {
	return "Available commands:\r\n" +
		"  show version                          - Display system version\r\n" +
		"  show wireless client summary           - Display wireless client summary\r\n" +
		"  show wireless client mac-address <mac>  - Display client detail\r\n" +
		"  show ip interface brief                - Display interface summary\r\n" +
		"  show ap summary                        - Display AP summary\r\n" +
		"  show wlan summary                      - Display WLAN summary\r\n" +
		"  exit                                   - Close connection\r\n"
}

func showVersion(dev *device.Device) string {
	totalClients := 0
	for _, ap := range dev.APs {
		totalClients += len(ap.Clients)
	}

	return fmt.Sprintf("Cisco IOS XE Software, Version %s\r\n"+
		"Cisco IOS Software [Dublin], C9800-CL Software (%s), Version %s\r\n"+
		"Copyright (c) 1986-2024 by Cisco Systems, Inc.\r\n"+
		"\r\n"+
		"ROM: IOS-XE ROMMON\r\n"+
		"\r\n"+
		"%s uptime is 45 days, 3 hours, 22 minutes\r\n"+
		"System returned to ROM by reload\r\n"+
		"\r\n"+
		"System image file is \"bootflash:packages.conf\"\r\n"+
		"\r\n"+
		"cisco %s (%s) processor with 8388608K/6147K bytes of memory.\r\n"+
		"Processor board ID %s\r\n"+
		"1 Virtual Ethernet interface\r\n"+
		"%d APs, %d Clients\r\n"+
		"\r\n"+
		"Configuration register is 0x2102\r\n",
		dev.Version, dev.Model, dev.Version,
		dev.Hostname,
		dev.Model, dev.Model, dev.Serial,
		len(dev.APs), totalClients)
}

func showWirelessClientSummary(dev *device.Device) string {
	clients := dev.AllClients()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Number of Clients: %d\r\n\r\n", len(clients)))
	sb.WriteString(fmt.Sprintf("%-20s %-16s %-8s %-6s %-20s %-18s %-6s\r\n",
		"MAC Address", "AP Name", "WLAN", "State", "Protocol", "Method", "RSSI"))
	sb.WriteString(strings.Repeat("-", 100) + "\r\n")
	for _, cwa := range clients {
		c := cwa.Client
		state := "Run"
		if c.State != "co-client-run" {
			state = c.State
		}
		proto := radioTypeShort(c.RadioType)
		sb.WriteString(fmt.Sprintf("%-20s %-16s %-8d %-6s %-20s %-18s %-6d\r\n",
			c.MAC, cwa.AP.Name, c.WLANId, state, proto, c.AuthKeyMgmt, c.RSSI))
	}
	return sb.String()
}

func showWirelessClientDetail(dev *device.Device, mac string) string {
	mac = strings.ToLower(mac)
	for _, cwa := range dev.AllClients() {
		if strings.ToLower(cwa.Client.MAC) == mac {
			c := cwa.Client
			return fmt.Sprintf("Client MAC Address: %s\r\n"+
				"  Client IPv4 Address:       %s\r\n"+
				"  Client Username:           %s\r\n"+
				"  Hostname:                  %s\r\n"+
				"  AP MAC Address:            %s\r\n"+
				"  AP Name:                   %s\r\n"+
				"  SSID:                      %s\r\n"+
				"  WLAN Id:                   %d\r\n"+
				"  Channel:                   %d\r\n"+
				"  Radio Type:                %s\r\n"+
				"  Security:                  %s / %s\r\n"+
				"  Auth Method:               %s\r\n"+
				"  RSSI:                      %d dBm\r\n"+
				"  SNR:                       %d dB\r\n"+
				"  Speed:                     %d Mbps\r\n"+
				"  Spatial Streams:           %d\r\n"+
				"  VLAN:                      %d (%s)\r\n"+
				"  Bytes Rx/Tx:               %d / %d\r\n"+
				"  Packets Rx/Tx:             %d / %d\r\n"+
				"  Policy Profile:            %s\r\n"+
				"  Device Type:               %s\r\n"+
				"  Device OS:                 %s\r\n"+
				"  State:                     %s\r\n",
				c.MAC, c.IPv4, c.Username, c.Hostname,
				cwa.AP.MAC, cwa.AP.Name,
				c.SSID, c.WLANId, c.Channel,
				radioTypeShort(c.RadioType),
				c.SecurityMode, c.EncryptionType,
				c.AuthKeyMgmt,
				c.RSSI, c.SNR, c.Speed, c.SpatialStreams,
				c.VLAN, c.VLANName,
				c.BytesRx, c.BytesTx,
				c.PktsRx, c.PktsTx,
				c.PolicyProfile,
				orDefault(c.DeviceType, "Unknown"),
				orDefault(c.DeviceOS, "Unknown"),
				c.State)
		}
	}
	return fmt.Sprintf("%% Client %s not found.\r\n", mac)
}

func showIPInterfaceBrief(dev *device.Device) string {
	return fmt.Sprintf("%-24s %-16s %-8s %-22s %-10s %-8s\r\n"+
		"%-24s %-16s %-8s %-22s %-10s %-8s\r\n"+
		"%-24s %-16s %-8s %-22s %-10s %-8s\r\n",
		"Interface", "IP-Address", "OK?", "Method", "Status", "Protocol",
		"GigabitEthernet1", dev.IP, "YES", "manual", "up", "up",
		"Loopback0", dev.IP, "YES", "manual", "up", "up")
}

func showAPSummary(dev *device.Device) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Number of APs: %d\r\n\r\n", len(dev.APs)))
	sb.WriteString(fmt.Sprintf("%-20s %-8s %-18s %-16s %-8s\r\n",
		"AP Name", "Slots", "AP Model", "Ethernet MAC", "Clients"))
	sb.WriteString(strings.Repeat("-", 75) + "\r\n")
	for _, ap := range dev.APs {
		model := ap.Model
		if model == "" {
			model = "C9120AXI-B"
		}
		sb.WriteString(fmt.Sprintf("%-20s %-8d %-18s %-16s %-8d\r\n",
			ap.Name, 2, model, ap.MAC, len(ap.Clients)))
	}
	return sb.String()
}

func showWLANSummary(dev *device.Device) string {
	// Collect unique SSIDs
	ssids := map[string]int{}
	for _, ap := range dev.APs {
		for _, c := range ap.Clients {
			ssids[c.SSID]++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Number of WLANs: %d\r\n\r\n", len(ssids)))
	sb.WriteString(fmt.Sprintf("%-6s %-32s %-10s %-10s\r\n", "ID", "Profile Name/SSID", "Status", "Clients"))
	sb.WriteString(strings.Repeat("-", 62) + "\r\n")
	id := 1
	for ssid, count := range ssids {
		sb.WriteString(fmt.Sprintf("%-6d %-32s %-10s %-10d\r\n", id, ssid, "Enabled", count))
		id++
	}
	return sb.String()
}

func showRunningConfig(dev *device.Device) string {
	raw := dev.RunningConfig()
	display := strings.ReplaceAll(raw, "\n", "\r\n")
	return fmt.Sprintf("Building configuration...\r\n\r\nCurrent configuration : %d bytes\r\n%s", len(raw), display)
}

func showStartupConfig(dev *device.Device) string {
	raw := dev.StartupConfig()
	display := strings.ReplaceAll(raw, "\n", "\r\n")
	return fmt.Sprintf("Using %d out of 2097152 bytes\r\n%s", len(raw), display)
}

func showSNMP(dev *device.Device) string {
	return fmt.Sprintf("Chassis: %s\r\n"+
		"Contact: admin@%s.local\r\n"+
		"Location: %s\r\n"+
		"%d SNMP packets input\r\n"+
		"    0 Bad SNMP version errors\r\n"+
		"    0 Unknown community name\r\n"+
		"    0 Illegal operation for community name supplied\r\n"+
		"    0 Encoding errors\r\n"+
		"    0 Number of requested variables\r\n"+
		"    0 Number of altered variables\r\n"+
		"    0 Get-request PDUs\r\n"+
		"    0 Get-next PDUs\r\n"+
		"    0 Set-request PDUs\r\n"+
		"    0 Input queue packet drops (Maximum queue size 1000)\r\n"+
		"%d SNMP packets output\r\n"+
		"    0 Too big errors (Maximum packet size 1500)\r\n"+
		"    0 No such name errors\r\n"+
		"    0 Bad values errors\r\n"+
		"    0 General errors\r\n"+
		"    0 Response PDUs\r\n"+
		"    0 Trap PDUs\r\n"+
		"SNMP Dispatcher:\r\n"+
		"    queue 0/75 (current/max), 0 dropped\r\n"+
		"SNMP Engine:\r\n"+
		"    drops 0\r\n"+
		"SNMP logging: enabled\r\n",
		dev.Serial, dev.Hostname, dev.Hostname, 0, 0)
}

func showInventory(dev *device.Device) string {
	return fmt.Sprintf("NAME: \"Chassis\", DESCR: \"Cisco Catalyst 9800-CL Wireless Controller\"\r\n"+
		"PID: %s          , VID: V01  , SN: %s\r\n"+
		"\r\n"+
		"NAME: \"module 0\", DESCR: \"Cisco Catalyst 9800-CL Wireless Controller\"\r\n"+
		"PID: %s          , VID:      , SN:            \r\n"+
		"\r\n"+
		"NAME: \"module R0\", DESCR: \"Cisco Catalyst 9800-CL Wireless Controller\"\r\n"+
		"PID: %s          , VID: V01  , SN: %s\r\n"+
		"\r\n"+
		"NAME: \"module F0\", DESCR: \"Cisco Catalyst 9800-CL Wireless Controller\"\r\n"+
		"PID: %s          , VID:      , SN:            \r\n",
		dev.Model, dev.Serial, dev.Model, dev.Model, dev.Serial, dev.Model)
}

func showDiag(dev *device.Device) string {
	return fmt.Sprintf("Chassis type: %s\r\n"+
		"\r\n"+
		"Slot 0:\r\n"+
		"  Main CPU:\r\n"+
		"    Hardware Revision     : 1.0\r\n"+
		"    Board Revision        : 0\r\n"+
		"    Part Number           : 73-19923-02\r\n"+
		"    Board Serial Number   : %s\r\n"+
		"    Top Assy. Part Number : 68-6190-02\r\n"+
		"    PID                   : %s\r\n"+
		"    VID                   : V01\r\n"+
		"    Processor type        : x86_64\r\n"+
		"    CLEI Code             : N/A\r\n",
		dev.Model, dev.Serial, dev.Model)
}

func showInstallRunning(dev *device.Device) string {
	return fmt.Sprintf("Type  Profile       State\r\n"+
		"--------------------------------------------------------------\r\n"+
		"IMG   default       Running\r\n"+
		"\r\n"+
		"Packages:\r\n"+
		"  bootflash:packages.conf\r\n"+
		"    Provisioning file: /bootflash/packages.conf\r\n"+
		"    File: /bootflash/%s.%s.SPA.bin\r\n"+
		"    Built: 2024-01-15_01.00\r\n"+
		"    Package: rpboot, version: %s\r\n"+
		"    Package: rpbase, version: %s\r\n"+
		"    Package: srdriver, version: %s\r\n",
		dev.Model, dev.Version, dev.Version, dev.Version, dev.Version)
}

func showInterfaces(dev *device.Device) string {
	mac := fmt.Sprintf("02c9.%02x%02x.%02x%02x",
		dev.IP[0]&0xff, dev.IP[1]&0xff, dev.IP[2]&0xff, dev.IP[3]&0xff)
	// Use a simpler approach for the MAC
	ipParts := strings.Split(dev.IP, ".")
	if len(ipParts) == 4 {
		mac = fmt.Sprintf("02c9.%s%s.%s%s",
			fmt.Sprintf("%02s", ipParts[0]), fmt.Sprintf("%02s", ipParts[1]),
			fmt.Sprintf("%02s", ipParts[2]), fmt.Sprintf("%02s", ipParts[3]))
	}
	_ = mac

	return fmt.Sprintf("GigabitEthernet1 is up, line protocol is up\r\n"+
		"  Hardware is CSR vNIC, address is 0200.0c9a.0001 (bia 0200.0c9a.0001)\r\n"+
		"  Internet address is %s/24\r\n"+
		"  MTU 1500 bytes, BW 1000000 Kbit/sec, DLY 10 usec,\r\n"+
		"     reliability 255/255, txload 1/255, rxload 1/255\r\n"+
		"  Encapsulation ARPA, loopback not set\r\n"+
		"  Keepalive set (10 sec)\r\n"+
		"  Full Duplex, 1000Mbps, link type is auto, media type is Virtual\r\n"+
		"  output flow-control is unsupported, input flow-control is unsupported\r\n"+
		"  ARP type: ARPA, ARP Timeout 04:00:00\r\n"+
		"  Last input 00:00:00, output 00:00:01, output hang never\r\n"+
		"  Last clearing of \"show interface\" counters never\r\n"+
		"  Input queue: 0/375/0/0 (size/max/drops/flushes); Total output drops: 0\r\n"+
		"  Queueing strategy: fifo\r\n"+
		"  Output queue: 0/40 (size/max)\r\n"+
		"  5 minute input rate 1000 bits/sec, 1 packets/sec\r\n"+
		"  5 minute output rate 1000 bits/sec, 1 packets/sec\r\n"+
		"     12345 packets input, 1234567 bytes, 0 no buffer\r\n"+
		"     Received 0 broadcasts (0 IP multicasts)\r\n"+
		"     0 runts, 0 giants, 0 throttles\r\n"+
		"     0 input errors, 0 CRC, 0 frame, 0 overrun, 0 ignored\r\n"+
		"     0 watchdog, 0 multicast, 0 pause input\r\n"+
		"     12345 packets output, 1234567 bytes, 0 underruns\r\n"+
		"     0 output errors, 0 collisions, 0 interface resets\r\n"+
		"     0 unknown protocol drops\r\n"+
		"     0 babbles, 0 late collision, 0 deferred\r\n"+
		"     0 lost carrier, 0 no carrier, 0 pause output\r\n"+
		"     0 output buffer failures, 0 output buffers swapped out\r\n"+
		"Loopback0 is up, line protocol is up\r\n"+
		"  Hardware is Loopback\r\n"+
		"  Internet address is %s/32\r\n"+
		"  MTU 1514 bytes, BW 8000000 Kbit/sec, DLY 5000 usec,\r\n"+
		"     reliability 255/255, txload 1/255, rxload 1/255\r\n"+
		"  Encapsulation LOOPBACK, loopback not set\r\n"+
		"  Last input never, output never, output hang never\r\n"+
		"  Last clearing of \"show interface\" counters never\r\n",
		dev.IP, dev.IP)
}

func showVRF() string {
	return "  Name                             Default RD            Protocols   Interfaces\r\n"
}

func showVLAN(dev *device.Device) string {
	vlans := map[int]string{}
	for _, ap := range dev.APs {
		for _, c := range ap.Clients {
			if c.VLAN > 0 {
				name := c.VLANName
				if name == "" {
					name = fmt.Sprintf("VLAN%04d", c.VLAN)
				}
				vlans[c.VLAN] = name
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("VLAN Name                             Status    Ports\r\n")
	sb.WriteString("---- -------------------------------- --------- -------------------------------\r\n")
	sb.WriteString("1    default                          active    \r\n")
	for id, name := range vlans {
		sb.WriteString(fmt.Sprintf("%-4d %-32s %-9s \r\n", id, name, "active"))
	}
	sb.WriteString("1002 fddi-default                     act/unsup \r\n")
	sb.WriteString("1003 trcrf-default                    act/unsup \r\n")
	sb.WriteString("1004 fddinet-default                  act/unsup \r\n")
	sb.WriteString("1005 trbrf-default                    act/unsup \r\n")
	return sb.String()
}

func showVTPStatus(dev *device.Device) string {
	return fmt.Sprintf("VTP Version capable             : 1 to 3\r\n"+
		"VTP version running             : 1\r\n"+
		"VTP Domain Name                 : \r\n"+
		"VTP Pruning Mode                : Disabled\r\n"+
		"VTP Traps Generation            : Disabled\r\n"+
		"Device ID                       : %s\r\n"+
		"Configuration last modified by %s at 0-0-00 00:00:00\r\n"+
		"Local updater ID is %s on interface Gi1 (lowest numbered VLAN interface found)\r\n"+
		"\r\n"+
		"Feature VLAN:\r\n"+
		"-----------\r\n"+
		"VTP Operating Mode                : Transparent\r\n"+
		"Maximum VLANs supported locally   : 1005\r\n"+
		"Number of existing VLANs          : 7\r\n"+
		"Configuration Revision            : 0\r\n"+
		"MD5 digest                        : 0x00 0x00 0x00 0x00 0x00 0x00 0x00 0x00\r\n",
		dev.Serial, dev.IP, dev.IP)
}

func showClock() string {
	now := time.Now()
	return fmt.Sprintf("%s\r\n", now.Format("15:04:05.000 MST Mon Jan 2 2006"))
}

func showNTP() string {
	return "NTP is not enabled.\r\n"
}

func showDir(parts []string) string {
	if len(parts) >= 2 {
		file := parts[1]
		return fmt.Sprintf("%% Error opening %s (No such file or directory)\r\n", file)
	}
	return "Directory of bootflash:/\r\n\r\n" +
		"  11  -rw-       4096  Jan 15 2024 01:00:00 +00:00  packages.conf\r\n" +
		"  12  -rw-   83886080  Jan 15 2024 01:00:00 +00:00  C9800-CL-K9.bin\r\n" +
		"\r\n" +
		"2097152000 bytes total (1900000000 bytes free)\r\n"
}

// handleInteractiveCopy handles copy commands with interactive Cisco-style
// prompts (Remote host, Destination filename) and performs the actual TFTP
// client push. Returns true if the command was handled.
// handleInteractiveCopy handles all copy commands with interactive Cisco-style
// prompts and performs actual TFTP client push when applicable.
// Handles: copy <any-source> tftp, copy <any-source> scp,
//          copy <source> tftp://host/file
// Returns true if the command was handled.
func handleInteractiveCopy(cmd string, channel ssh.Channel, dev *device.Device, tftpMgr *tftpsim.Manager, logCmd func(string)) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 || !matchCmd(parts, "copy") {
		return false
	}

	config := dev.RunningConfig()
	source := parts[1]

	// Case: copy <source> tftp://host/file (full URL)
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "tftp://") {
			tftpMgr.EnsureRunning(dev)
			u := strings.TrimPrefix(p, "tftp://")
			slash := strings.Index(u, "/")
			if slash > 0 {
				host := u[:slash]
				filename := u[slash+1:]
				resp := readLinePrompt(channel, fmt.Sprintf("Destination filename [%s]? ", filename))
				if resp == "" {
					resp = filename
				}
				logCmd(fmt.Sprintf("tftp-push %s -> %s:%s", source, host, resp))
				pushTFTP(host, resp, config)
			}
			channel.Write([]byte(fmt.Sprintf("!!\r\n%d bytes copied in 0.5 secs (%d bytes/sec)\r\n", len(config), len(config)*2)))
			return true
		}
	}

	// Case: copy <source> tftp (interactive)
	if len(parts) >= 3 && strings.ToLower(parts[2]) == "tftp" {
		tftpMgr.EnsureRunning(dev)
		host := readLinePrompt(channel, "Address or name of remote host []? ")
		defName := strings.ToLower(dev.Hostname) + "-confg"
		filename := readLinePrompt(channel, fmt.Sprintf("Destination filename [%s]? ", defName))
		if filename == "" {
			filename = defName
		}
		logCmd(fmt.Sprintf("tftp-push %s -> %s:%s", source, host, filename))
		pushTFTP(host, filename, config)
		channel.Write([]byte(fmt.Sprintf("!!\r\n%d bytes copied in 0.5 secs (%d bytes/sec)\r\n", len(config), len(config)*2)))
		return true
	}

	// Case: copy <source> scp (interactive)
	if len(parts) >= 3 && strings.ToLower(parts[2]) == "scp" {
		host := readLinePrompt(channel, "Address or name of remote host []? ")
		defName := strings.ToLower(dev.Hostname) + "-confg"
		filename := readLinePrompt(channel, fmt.Sprintf("Destination filename [%s]? ", defName))
		if filename == "" {
			filename = defName
		}
		logCmd(fmt.Sprintf("scp-push %s -> %s:%s", source, host, filename))
		channel.Write([]byte(fmt.Sprintf("!!\r\n%d bytes copied in 0.3 secs (%d bytes/sec)\r\n", len(config), len(config)*3)))
		return true
	}

	return false
}

// readLinePrompt writes a prompt to the channel and reads a line of input,
// echoing characters as they're typed.
func readLinePrompt(channel ssh.Channel, prompt string) string {
	channel.Write([]byte(prompt))
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := channel.Read(buf)
		if err != nil || n == 0 {
			return string(line)
		}
		ch := buf[0]
		switch ch {
		case '\r', '\n':
			channel.Write([]byte("\r\n"))
			return strings.TrimSpace(string(line))
		case 127, '\b':
			if len(line) > 0 {
				line = line[:len(line)-1]
				channel.Write([]byte("\b \b"))
			}
		default:
			line = append(line, ch)
			channel.Write([]byte{ch})
		}
	}
}

// pushTFTP sends the config to a remote TFTP server as a client.
func pushTFTP(host, filename, config string) {
	if host == "" {
		return
	}
	addr := host
	if !strings.Contains(addr, ":") {
		addr = host + ":69"
	}
	c, err := tftp.NewClient(addr)
	if err != nil {
		return
	}
	rf, err := c.Send(filename, "octet")
	if err != nil {
		return
	}
	rf.ReadFrom(strings.NewReader(config))
}

// serveSFTP handles SFTP subsystem requests using a virtual filesystem
// that serves the device's running-config.
func serveSFTP(channel ssh.Channel, dev *device.Device) {
	handler := &virtualFS{files: map[string]string{
		"running-config": dev.RunningConfig(),
		"startup-config": dev.StartupConfig(),
	}}
	server := sftp.NewRequestServer(channel, sftp.Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	})
	server.Serve()
	server.Close()
}

// virtualFS implements sftp.Handlers to serve config files.
type virtualFS struct {
	files map[string]string
}

func (vfs *virtualFS) resolve(path string) (string, bool) {
	name := strings.TrimPrefix(path, "/")
	content, ok := vfs.files[name]
	return content, ok
}

func (vfs *virtualFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if content, ok := vfs.resolve(r.Filepath); ok {
		return strings.NewReader(content), nil
	}
	return nil, os.ErrNotExist
}

func (vfs *virtualFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}

func (vfs *virtualFS) Filecmd(r *sftp.Request) error {
	return nil
}

func (vfs *virtualFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "Stat":
		if r.Filepath == "/" || r.Filepath == "." {
			// Return first file for root stat
			for name, content := range vfs.files {
				return listAt([]os.FileInfo{&virtualFileInfo{name: name, size: int64(len(content))}}), nil
			}
		}
		if content, ok := vfs.resolve(r.Filepath); ok {
			name := strings.TrimPrefix(r.Filepath, "/")
			return listAt([]os.FileInfo{&virtualFileInfo{name: name, size: int64(len(content))}}), nil
		}
		return nil, os.ErrNotExist
	case "List":
		var infos []os.FileInfo
		for name, content := range vfs.files {
			infos = append(infos, &virtualFileInfo{name: name, size: int64(len(content))})
		}
		return listAt(infos), nil
	default:
		return nil, sftp.ErrSSHFxPermissionDenied
	}
}

type listAt []os.FileInfo

func (l listAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n+int(offset) >= len(l) {
		return n, io.EOF
	}
	return n, nil
}

type virtualFileInfo struct {
	name string
	size int64
}

func (fi *virtualFileInfo) Name() string      { return fi.name }
func (fi *virtualFileInfo) Size() int64       { return fi.size }
func (fi *virtualFileInfo) Mode() fs.FileMode { return 0644 }
func (fi *virtualFileInfo) ModTime() time.Time { return time.Now() }
func (fi *virtualFileInfo) IsDir() bool        { return false }
func (fi *virtualFileInfo) Sys() interface{}   { return nil }

// handleSCP implements the SCP wire protocol for file download (-f flag).
// Returns true if the command was an SCP request and was handled.
func handleSCP(cmd string, channel ssh.Channel, dev *device.Device) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 || parts[0] != "scp" {
		return false
	}

	// Look for -f (from/download) flag
	sendFile := false
	for _, p := range parts {
		if p == "-f" {
			sendFile = true
		}
	}
	if !sendFile {
		return false
	}

	// Determine which file was requested (last non-flag argument)
	filename := "running-config"
	for _, p := range parts[1:] {
		if !strings.HasPrefix(p, "-") {
			filename = p
		}
	}
	var config string
	switch filename {
	case "startup-config":
		config = dev.StartupConfig()
	default:
		config = dev.RunningConfig()
	}

	// SCP protocol:
	// 1. Wait for client to send \0 (ready)
	buf := make([]byte, 1)
	channel.Read(buf)

	// 2. Send file header: C<mode> <size> <filename>\n
	header := fmt.Sprintf("C0644 %d %s\n", len(config), filename)
	channel.Write([]byte(header))

	// 3. Wait for client ACK
	channel.Read(buf)

	// 4. Send file content
	channel.Write([]byte(config))

	// 5. Send \0 (transfer complete)
	channel.Write([]byte{0})

	// 6. Wait for client ACK
	channel.Read(buf)

	return true
}

func radioTypeShort(rt string) string {
	switch rt {
	case "client-radio-type-11ax-5ghz":
		return "802.11ax - 5 GHz"
	case "client-radio-type-11ax-24ghz":
		return "802.11ax - 2.4 GHz"
	case "client-radio-type-11ac":
		return "802.11ac"
	case "client-radio-type-11n-5ghz":
		return "802.11n - 5 GHz"
	case "client-radio-type-11n-24ghz":
		return "802.11n - 2.4 GHz"
	default:
		return rt
	}
}

func orDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}

func generateHostKey() (ssh.Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return ssh.ParsePrivateKey(keyPEM)
}
