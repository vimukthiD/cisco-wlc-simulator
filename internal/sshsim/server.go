package sshsim

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/vimukthi/cisco-wlc-sim/internal/config"
	"github.com/vimukthi/cisco-wlc-sim/internal/device"
	"golang.org/x/crypto/ssh"
)

// Serve starts an SSH server for the given device.
func Serve(dev *device.Device, auth config.Auth) error {
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
		go handleConnection(conn, sshConfig, dev)
	}
}

func handleConnection(conn net.Conn, config *ssh.ServerConfig, dev *device.Device) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()

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

		go handleSession(channel, requests, dev)
	}
}

func handleSession(channel ssh.Channel, requests <-chan *ssh.Request, dev *device.Device) {
	defer channel.Close()

	// Handle session requests (pty, shell, exec)
	for req := range requests {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			runShell(channel, dev)
			return
		case "exec":
			req.Reply(true, nil)
			// Extract command from exec payload
			if len(req.Payload) > 4 {
				cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
				if cmdLen <= len(req.Payload)-4 {
					cmd := string(req.Payload[4 : 4+cmdLen])
					output := executeCommand(cmd, dev)
					channel.Write([]byte(output))
				}
			}
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func runShell(channel ssh.Channel, dev *device.Device) {
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
					output := executeCommand(cmd, dev)
					channel.Write([]byte(output))
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

func executeCommand(cmd string, dev *device.Device) string {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
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
