package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

const (
	dashURL    = "http://localhost:8080"
	reset      = "\033[0m"
	bold       = "\033[1m"
	dim        = "\033[2m"
	cyan       = "\033[36m"
	green      = "\033[32m"
	yellow     = "\033[33m"
	blue       = "\033[34m"
	red        = "\033[31m"
	clearScr   = "\033[2J\033[H"
	hideCursor = "\033[?25l"
	showCursor = "\033[?25h"
)

type deviceInfo struct {
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	HTTPSPort int    `json:"https_port"`
	SSHPort   int    `json:"ssh_port"`
	SNMPPort  int    `json:"snmp_port"`
	Model     string `json:"model"`
	Version   string `json:"version"`
	APs       []struct {
		Name    string `json:"name"`
		Clients []any  `json:"clients"`
	} `json:"aps"`
}

type systemInfo struct {
	UptimeSecs   int     `json:"uptime_secs"`
	Goroutines   int     `json:"goroutines"`
	CPUCount     int     `json:"cpu_count"`
	CPUPct       float64 `json:"cpu_pct"`
	MemAlloc     uint64  `json:"mem_alloc"`
	MemSys       uint64  `json:"mem_sys"`
	MemHeapAlloc uint64  `json:"mem_heap_alloc"`
}

type logEntry struct {
	Timestamp  string `json:"timestamp"`
	DeviceHost string `json:"device_host"`
	Type       string `json:"type"`
	Source     string `json:"source"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Command    string `json:"command"`
	Detail     string `json:"detail"`
	User       string `json:"user"`
	Status     int    `json:"status"`
}

var renderPaused atomic.Bool

func main() {
	fmt.Print(hideCursor)
	defer fmt.Print(showCursor)

	go handleKeys()

	for {
		if !renderPaused.Load() {
			render()
		}
		time.Sleep(3 * time.Second)
	}
}

func render() {
	var sb strings.Builder

	sb.WriteString(clearScr)
	sb.WriteString(fmt.Sprintf("%s%s", bold, cyan))
	sb.WriteString("  ╔══════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("  ║          Cisco 9800-CL WLC Simulator Appliance              ║\n")
	sb.WriteString("  ╚══════════════════════════════════════════════════════════════╝\n")
	sb.WriteString(reset)
	sb.WriteString("\n")

	// System info
	sys := fetchSystem()
	if sys != nil {
		uptime := formatUptime(sys.UptimeSecs)
		mem := formatBytes(sys.MemHeapAlloc)
		sb.WriteString(fmt.Sprintf("  %sSystem%s  Uptime: %s%s%s  CPU: %s%.1f%%%s  Memory: %s%s%s  Goroutines: %d\n",
			bold, reset, green, uptime, reset, yellow, sys.CPUPct, reset, blue, mem, reset, sys.Goroutines))
	} else {
		sb.WriteString(fmt.Sprintf("  %sSystem%s  %sWaiting for simulator to start...%s\n", bold, reset, dim, reset))
	}

	// Network info
	iface, ip := getNetworkInfo()
	bareIP := ip
	if idx := strings.Index(bareIP, "/"); idx >= 0 {
		bareIP = bareIP[:idx]
	}
	sb.WriteString(fmt.Sprintf("  %sNetwork%s Interface: %s%s%s  VM IP: %s%s%s\n",
		bold, reset, cyan, iface, reset, green+bold, bareIP, reset))
	sb.WriteString(fmt.Sprintf("  %sDashboard%s %shttp://%s:8080%s\n", bold, reset, cyan, bareIP, reset))
	sb.WriteString("\n")

	// Devices
	devices := fetchDevices()
	sb.WriteString(fmt.Sprintf("  %s%sSimulated Devices%s\n", bold, cyan, reset))
	sb.WriteString(fmt.Sprintf("  %s%-20s %-18s %-14s %-8s %-6s %-8s%s\n",
		dim, "HOSTNAME", "IP", "MODEL", "APs", "CLNTS", "STATUS", reset))
	sb.WriteString(fmt.Sprintf("  %s%s%s\n", dim, strings.Repeat("─", 76), reset))

	if len(devices) == 0 {
		sb.WriteString(fmt.Sprintf("  %sNo devices configured%s\n", dim, reset))
	}
	for _, d := range devices {
		clientCount := 0
		for _, ap := range d.APs {
			clientCount += len(ap.Clients)
		}
		sb.WriteString(fmt.Sprintf("  %-20s %s%-18s%s %-14s %-8d %-6d %s●%s Online%s\n",
			d.Hostname, bold, d.IP, reset, d.Model, len(d.APs), clientCount, green, reset+dim, reset))
	}
	sb.WriteString("\n")

	// Recent logs
	logs := fetchLogs()
	sb.WriteString(fmt.Sprintf("  %s%sRecent Activity%s\n", bold, cyan, reset))
	if len(logs) == 0 {
		sb.WriteString(fmt.Sprintf("  %sWaiting for requests...%s\n", dim, reset))
	}
	for i, l := range logs {
		if i >= 8 {
			break
		}
		ts := ""
		if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
			ts = t.Format("15:04:05")
		}
		typeColor := dim
		switch l.Type {
		case "restconf":
			typeColor = blue
		case "ssh":
			typeColor = cyan
		case "snmp":
			typeColor = yellow
		case "tftp":
			typeColor = green
		}
		detail := l.Path
		if l.Command != "" {
			detail = "$ " + l.Command
		} else if l.Detail != "" {
			detail = l.Detail
		}
		sb.WriteString(fmt.Sprintf("  %s%s%s %s%-8s%s %-14s %s\n",
			dim, ts, reset, typeColor, strings.ToUpper(l.Type), reset, l.DeviceHost, detail))
	}

	sb.WriteString(fmt.Sprintf("\n  %s[r] reboot  [s] shutdown  [q] shell%s\n", dim, reset))

	// Raw terminal mode disables \n → \r\n translation, so add \r explicitly
	fmt.Print(strings.ReplaceAll(sb.String(), "\n", "\r\n"))
}

func handleKeys() {
	fd := int(os.Stdin.Fd())

	// Put terminal in raw mode using Go's term package (reliable under inittab)
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// If stdin isn't a terminal, try opening /dev/tty directly
		tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if ttyErr != nil {
			return
		}
		defer tty.Close()
		fd = int(tty.Fd())
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return
		}
	}
	_ = oldState // kept for restore

	buf := make([]byte, 1)
	for {
		n, err := os.NewFile(uintptr(fd), "tty").Read(buf)
		if err != nil || n == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		switch buf[0] {
		case 'q':
			// Drop to shell
			renderPaused.Store(true)
			term.Restore(fd, oldState)
			fmt.Print(showCursor + clearScr)
			fmt.Println("Dropped to shell. Type 'exit' to return to console.\r")
			cmd := exec.Command("/bin/sh")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			fmt.Print(hideCursor)
			oldState, _ = term.MakeRaw(fd)
			renderPaused.Store(false)
		case 'r':
			// Reboot the VM
			term.Restore(fd, oldState)
			fmt.Print(showCursor + clearScr)
			fmt.Println("Rebooting...")
			exec.Command("/sbin/reboot").Run()
		case 's':
			// Shutdown the VM
			term.Restore(fd, oldState)
			fmt.Print(showCursor + clearScr)
			fmt.Println("Shutting down...")
			exec.Command("/sbin/poweroff").Run()
		}
	}
}

func fetchJSON(path string, v any) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(dashURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

func fetchDevices() []deviceInfo {
	var devs []deviceInfo
	fetchJSON("/api/devices", &devs)
	return devs
}

func fetchSystem() *systemInfo {
	var sys systemInfo
	if err := fetchJSON("/api/system", &sys); err != nil {
		return nil
	}
	return &sys
}

func fetchLogs() []logEntry {
	var logs []logEntry
	fetchJSON("/api/logs", &logs)
	// Reverse — most recent first
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs
}

func getNetworkInfo() (string, string) {
	// Auto-detect the primary non-loopback interface with an IPv4 address
	out, err := exec.Command("ip", "-4", "-o", "addr", "show").Output()
	if err != nil {
		return "unknown", "acquiring..."
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		iface := fields[1]
		if iface == "lo" {
			continue
		}
		for i, f := range fields {
			if f == "inet" && i+1 < len(fields) {
				return iface, fields[i+1]
			}
		}
	}
	return "unknown", "acquiring..."
}

func formatUptime(secs int) string {
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if h < 24 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dd %dh", h/24, h%24)
}

func formatBytes(b uint64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	if b >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
	if b >= 1<<10 {
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}
