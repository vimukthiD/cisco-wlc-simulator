# Cisco 9800-CL WLC Simulator

A lightweight simulator for Cisco Catalyst 9800-CL Wireless LAN Controllers. Simulates multiple WLC devices, each with its own IP address, RESTCONF API, SSH CLI, SNMP agent, SCP/SFTP/TFTP file transfer, and a real-time web dashboard.

Inspired by [simsnmp](https://github.com/lfbayer/simsnmp) — one IP per simulated device, data-driven configuration.

## Features

- **RESTCONF API** — client-oper-data and access-point-oper-data endpoints with XML (default) and JSON content negotiation
- **SSH CLI** — Cisco IOS-XE style shell with 20+ show commands, interactive `copy` dialogs, `enable`, `terminal length 0`
- **SNMP Agent** — SNMPv2c with system, entity, interface, and IP address MIBs (30+ OIDs)
- **SCP / SFTP** — download running-config and startup-config via SCP (both modern SFTP and legacy protocol)
- **TFTP** — on-demand TFTP server for config transfers, starts when `copy` command is issued
- **Web Dashboard** — real-time view of devices, APs, clients, system metrics, and live access logs
- **Runtime Management** — add/remove devices, APs, and clients via dashboard or REST API while running
- **LAN Mode** — bind to physical interface for network-wide accessibility from other machines
- **Multiple Devices** — each device gets its own IP on standard ports (SSH 22, HTTPS 443, SNMP 161)
- **Config Template** — customizable running-config template with device-specific values
- **Auto IP Lifecycle** — IPs set up on startup, cleaned up on shutdown

## Quick Start

```bash
# Build
go build -o wlcsim ./cmd/wlcsim/

# Run (local-only mode — IPs on loopback)
sudo ./wlcsim -config configs/devices.yaml

# Run (LAN mode — accessible from other machines)
sudo ./wlcsim -lan -config configs/devices.yaml

# Open the dashboard
open http://localhost:8080
```

## Usage

### Network Modes

**Local-only (default):** Devices use loopback aliases — accessible only from the host machine.

```bash
sudo ./wlcsim -config configs/devices.yaml
# Devices at 10.99.0.1, 10.99.0.2 (from config)
```

**LAN mode (`-lan`):** Devices use physical interface aliases with auto-assigned IPs from the LAN subnet. Other machines on the network can reach the simulated devices.

```bash
sudo ./wlcsim -lan -config configs/devices.yaml
# Auto-detects en0, assigns 192.168.1.200, 192.168.1.201, etc.

# Override interface
sudo ./wlcsim -lan -interface en1 -config configs/devices.yaml
```

In LAN mode, config IPs outside the LAN subnet are automatically reassigned.

### RESTCONF API

Supports both XML (default) and JSON via `Accept` header:

```bash
# Client operational data (XML default)
curl -sk -u admin:Cisco123 \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# JSON format
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# Sub-endpoints
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/common-oper-data
  # Also: dot11-oper-data, traffic-stats, sisf-db-mac, dc-info, policy-data

# AP operational data (returns all APs regardless of client count)
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-access-point-oper:access-point-oper-data
  # Sub-endpoints: capwap-data, ap-name-mac-map, radio-oper-data
```

### SSH CLI

```bash
ssh admin@10.99.0.1
# Password: Cisco123
```

Supported commands:
- `show version`, `show running-config`, `show startup-config`
- `show wireless client summary`, `show wireless client mac-address <mac>`
- `show ap summary`, `show wlan summary`
- `show ip interface brief`, `show interfaces`
- `show snmp`, `show inventory`, `show diag`, `show diagbus`
- `show vlan`, `show vtp status`, `show vrf`
- `show install running`, `show sdwan running-config`
- `show standby`, `show vrrp`, `show glbp`, `show clock`
- `copy running-config tftp` / `copy startup-config tftp` (interactive with prompts)
- `copy nvram tftp://host/file`
- `enable`, `terminal length 0`, `configure terminal`, `dir`
- `admin show version` (admin prefix stripped)

### SNMP

```bash
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.1      # System MIB
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.2      # Interfaces
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.4.20.1  # IP address table
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.47      # Entity MIB
```

Supported MIBs:
- **SNMPv2-MIB** — sysDescr, sysObjectID (Catalyst 9800), sysUpTime, sysName, sysContact, sysLocation, sysServices
- **SNMP-FRAMEWORK-MIB** — snmpEngineTime
- **IF-MIB** — ifNumber, ifIndex, ifDescr, ifType, ifMtu, ifSpeed, ifPhysAddress, ifAdminStatus, ifOperStatus, ifName, ifAlias
- **IP-MIB** — ipAdEntAddr, ipAdEntIfIndex, ipAdEntNetMask
- **ENTITY-MIB** — entPhysicalDescr, entPhysicalSerialNum, entPhysicalModelName, entPhysicalSoftwareRev, entPhysicalMfgName

### SCP / SFTP / TFTP

```bash
# SCP download (modern SFTP mode)
scp admin@10.99.0.1:running-config ./config.txt

# SCP download (legacy mode)
scp -O admin@10.99.0.1:startup-config ./config.txt

# TFTP (on-demand — starts when copy command triggers it)
# From SSH: copy running-config tftp → enter remote host → enter filename
```

### Web Dashboard

```bash
open http://localhost:8080    # Default port
./wlcsim -dashboard-port 9090  # Custom port
```

Features:
- **System metrics** — CPU usage, memory, goroutines, uptime (auto-refreshing)
- **Device list** — all WLCs with add/delete buttons
- **Access Points** — per-device AP table with SSIDs, edit/delete
- **Clients** — per-device client table with RSSI bars, move/delete
- **Config tab** — credentials, RESTCONF URL, SSH command, SNMP community, SCP/TFTP examples
- **Live access logs** — real-time RESTCONF, SSH, SNMP, TFTP requests via SSE
- **Runtime management** — add devices/APs/clients with auto-generated values

### Dashboard REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/devices` | List all devices |
| POST | `/api/devices` | Add a new device (starts servers, adds IP alias) |
| DELETE | `/api/devices?ip=<ip>` | Remove a device |
| POST | `/api/devices/ap` | Add an AP to a device |
| DELETE | `/api/devices/ap?device_ip=<ip>&name=<name>` | Remove an AP |
| PUT | `/api/devices/ap/ssids` | Update AP SSIDs |
| POST | `/api/devices/client` | Add a client to an AP |
| DELETE | `/api/devices/client?device_ip=<ip>&mac=<mac>` | Remove a client |
| PUT | `/api/devices/client/move` | Move client to different AP/SSID |
| GET | `/api/auth` | Get credentials |
| GET | `/api/system` | System metrics (CPU, memory, uptime) |
| GET | `/api/logs` | Recent access log entries |
| GET | `/api/logs/stream` | SSE stream of new log entries |

## Configuration

All devices are defined in `configs/devices.yaml`:

```yaml
auth:
  username: admin
  password: Cisco123
  snmp_community: public

devices:
  - hostname: WLC-SITE-A
    ip: 10.99.0.1       # auto-reassigned in LAN mode
    https_port: 443
    ssh_port: 22
    aps:
      - name: AP-Floor1-Lobby
        mac: "00:3a:7d:12:01:00"
        ssids: ["Corporate-WiFi", "Guest-WiFi"]
        clients:
          - mac: "aa:bb:cc:11:22:01"
            ipv4: "10.10.50.101"
            username: jsmith
            ssid: Corporate-WiFi
            rssi: -45
            snr: 42
```

The running-config template can be customized by editing `configs/running-config.tmpl`.

See [configs/devices.yaml](configs/devices.yaml) for a full example with two devices, 4 APs, and 7 clients.

## Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `configs/devices.yaml` | Path to devices config file |
| `-dashboard-port` | `8080` | Web dashboard HTTP port |
| `-lan` | `false` | LAN mode — bind to physical interface for network accessibility |
| `-interface` | (auto-detect) | Network interface for LAN mode |
| `-setup-ips` | `false` | Only add IP aliases, then exit |
| `-teardown-ips` | `false` | Only remove IP aliases, then exit |

## Architecture

```
cmd/wlcsim/main.go              — Entry point, flags, signal handling, auto IP lifecycle
internal/
  config/config.go               — YAML config loading with defaults, template loading
  device/device.go               — Device, AP, Client models, config template rendering
  simulator/simulator.go         — Thread-safe device lifecycle (RWMutex), runtime add/remove
  accesslog/accesslog.go         — Thread-safe access log store with SSE pub/sub
  restconf/
    server.go                    — HTTPS server with self-signed TLS, basic auth, logging
    handlers.go                  — RESTCONF handlers: client-oper-data, access-point-oper-data
                                   XML/JSON content negotiation
  sshsim/server.go               — SSH server with IOS-XE CLI, SCP/SFTP, interactive copy
  snmp/server.go                 — SNMP agent: system, entity, interface, IP address MIBs
  tftpsim/server.go              — On-demand TFTP server with idle timeout, SO_REUSEPORT
  dashboard/
    server.go                    — Dashboard HTTP server, REST API, SSE, CPU sampler
    static/index.html            — Embedded SPA dashboard (HTML/CSS/JS)
  network/
    setup.go                     — IP alias management (loopback + physical interface)
    detect.go                    — Primary interface detection (macOS/Linux)
    arp.go                       — ARP probing, unused IP discovery, gratuitous ARP
configs/
  devices.yaml                   — Sample config with 2 WLCs, 4 APs, 7 clients
  running-config.tmpl            — Customizable Cisco IOS-XE config template
```

### How It Works

1. **Config loading**: YAML parsed, defaults applied, config template rendered per device
2. **IP setup**: Loopback aliases (default) or physical interface aliases (LAN mode) with gratuitous ARP
3. **Per-device servers**: Each device spawns HTTPS, SSH, and SNMP servers bound to its IP
4. **TFTP on-demand**: Started when SSH `copy` command is issued, shuts down after 30s idle
5. **RESTCONF**: XML by default, JSON when `Accept: application/yang-data+json` is set
6. **SSH CLI**: Interactive shell with command matching, `copy` dialogs with TFTP client push
7. **SCP/SFTP**: Virtual filesystem serving running-config and startup-config
8. **Access logging**: All protocol requests recorded and streamed to dashboard via SSE
9. **Dashboard**: Embedded web UI with runtime device/AP/client management
10. **Shutdown**: SIGINT/SIGTERM triggers IP alias cleanup
