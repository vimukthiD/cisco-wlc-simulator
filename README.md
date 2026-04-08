# Cisco 9800-CL WLC Simulator

A lightweight simulator for Cisco Catalyst 9800-CL Wireless LAN Controllers. Simulates multiple WLC devices, each with its own IP address, RESTCONF API, and SSH CLI.

Inspired by [simsnmp](https://github.com/lfbayer/simsnmp) — one IP per simulated device, data-driven configuration.

## Features

- **RESTCONF API** — `Cisco-IOS-XE-wireless-client-oper:client-oper-data` with all sub-endpoints (common-oper-data, dot11-oper-data, traffic-stats, sisf-db-mac, dc-info, policy-data)
- **SSH CLI** — Cisco IOS-XE style shell with `show` commands
- **SNMP Agent** — SNMPv2c with system, entity, and interface MIBs (device type, vendor, serial, model)
- **SCP / TFTP** — download running-config via SCP (SFTP + legacy) or TFTP
- **Web Dashboard** — real-time view of devices, clients, APs, and live access logs
- **Multiple Devices** — each device gets its own IP, all on standard ports (SSH 22, HTTPS 443, SNMP 161, TFTP 69)
- **Virtual IPs** — loopback aliases so each device is individually pingable
- **YAML Config** — define devices, APs, and clients in a single config file

## Quick Start

```bash
# Build
go build -o wlcsim ./cmd/wlcsim/

# Set up virtual IPs and run (sudo needed for ports 22/443 and IP aliases)
sudo ./wlcsim -setup-ips -config configs/devices.yaml

# Open the dashboard
open http://localhost:8080
```

## Usage

### Virtual IP Setup

Each simulated device binds to its own IP address. On macOS, these are loopback aliases:

```bash
# Add IPs (done automatically with -setup-ips)
sudo ifconfig lo0 alias 10.99.0.1
sudo ifconfig lo0 alias 10.99.0.2

# Verify they're pingable
ping -c 1 10.99.0.1
ping -c 1 10.99.0.2

# Remove IPs when done
sudo ./wlcsim -teardown-ips -config configs/devices.yaml
```

### RESTCONF API

All devices listen on the standard HTTPS port (443) on their own IP — just like real WLCs:

```bash
# Full client operational data
curl -sk -u admin:Cisco123 \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# Just wireless client common data
curl -sk -u admin:Cisco123 \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/common-oper-data

# Traffic stats
curl -sk -u admin:Cisco123 \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/traffic-stats

# Second device — same port, different IP
curl -sk -u admin:Cisco123 \
  https://10.99.0.2/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data
```

### SSH

All devices listen on standard SSH port (22):

```bash
ssh admin@10.99.0.1
ssh admin@10.99.0.2
# Password: Cisco123

# Available CLI commands:
#   show version
#   show wireless client summary
#   show wireless client mac-address <mac>
#   show ip interface brief
#   show ap summary
#   show wlan summary
```

### SNMP

All devices respond to SNMPv2c queries on the standard port (161):

```bash
# Walk system MIB — device identity
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.1

# Get device name
snmpget -v2c -c public 10.99.0.1 1.3.6.1.2.1.1.5.0

# Get serial number (ENTITY-MIB)
snmpget -v2c -c public 10.99.0.1 1.3.6.1.2.1.47.1.1.1.1.11.1

# Get model name
snmpget -v2c -c public 10.99.0.1 1.3.6.1.2.1.47.1.1.1.1.13.1

# Walk interfaces
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.2

# Second device — same port, different IP
snmpwalk -v2c -c public 10.99.0.2 1.3.6.1.2.1.1
```

Supported MIBs:
- **SNMPv2-MIB** — sysDescr, sysObjectID (Catalyst 9800), sysUpTime, sysName, sysContact, sysLocation, sysServices
- **ENTITY-MIB** — entPhysicalDescr, entPhysicalSerialNum, entPhysicalModelName, entPhysicalSoftwareRev, entPhysicalMfgName
- **IF-MIB** — ifNumber, ifIndex, ifDescr, ifType, ifPhysAddress, ifAdminStatus, ifOperStatus

### Web Dashboard

The dashboard runs on `http://localhost:8080` by default (configurable with `-dashboard-port`).

```bash
# Custom port
./wlcsim -config configs/devices.yaml -dashboard-port 9090
```

The dashboard provides:
- **Device overview** — all simulated WLCs with IP, model, version, AP/client counts
- **Client detail** — per-device table with MAC, IP, SSID, RSSI signal bars, speed, traffic
- **AP detail** — access points with model, client count, served SSIDs
- **Device config** — connection info (RESTCONF URL, SSH command)
- **Live access logs** — real-time stream of all RESTCONF and SSH requests via SSE

The dashboard also exposes JSON API endpoints:
- `GET /api/devices` — all device configurations
- `GET /api/logs` — recent access log entries
- `GET /api/logs/stream` — SSE stream of new log entries

## Configuration

All devices are defined in `configs/devices.yaml`:

```yaml
auth:
  username: admin
  password: Cisco123
  snmp_community: public

devices:
  - hostname: WLC-SITE-A
    ip: 10.99.0.1       # each device gets its own IP
    https_port: 443      # standard port — same across all devices
    ssh_port: 22         # standard port — same across all devices
    aps:
      - name: AP-Floor1-Lobby
        mac: "00:3a:7d:12:01:00"
        clients:
          - mac: "aa:bb:cc:11:22:01"
            ipv4: "10.10.50.101"
            username: jsmith
            ssid: Corporate-WiFi
            rssi: -45
            snr: 42
            # ... more fields
```

Each device binds to its own IP:port tuple, so multiple devices can share port 22 and 443. Ports below 1024 require running with `sudo`.

See [configs/devices.yaml](configs/devices.yaml) for a full example with two devices.

## Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `configs/devices.yaml` | Path to devices config file |
| `-dashboard-port` | `8080` | Web dashboard HTTP port |
| `-setup-ips` | `false` | Add virtual IP aliases (requires sudo) |
| `-teardown-ips` | `false` | Remove virtual IP aliases (requires sudo) |

## Architecture

```
cmd/wlcsim/main.go              — Entry point, flag parsing, signal handling
internal/
  config/config.go               — YAML config loading with defaults
  device/device.go               — Device, AP, Client data models
  accesslog/accesslog.go         — Thread-safe access log store with SSE pub/sub
  restconf/
    server.go                    — HTTPS server with self-signed TLS, basic auth, request logging
    handlers.go                  — RESTCONF endpoint handlers (YANG-format JSON)
  sshsim/server.go               — SSH server with Cisco IOS-XE CLI simulation, command logging
  snmp/server.go                 — SNMP agent with system, entity, and interface MIBs
  tftpsim/server.go              — TFTP server serving running-config
  dashboard/
    server.go                    — Dashboard HTTP server with JSON API and SSE
    static/index.html            — Embedded single-page dashboard (HTML/CSS/JS)
  network/setup.go               — Virtual IP management (macOS/Linux)
configs/
  devices.yaml                   — Sample config with 2 WLCs, 4 APs, 7 clients
```

### How It Works

1. **IP Setup** (`-setup-ips`): Adds loopback aliases via `ifconfig lo0 alias` (macOS) or `ip addr add` (Linux)
2. **Per-device servers**: Each device spawns its own HTTPS and SSH server, bound to its specific IP
3. **RESTCONF responses**: Built from the YAML config, formatted to match the real `Cisco-IOS-XE-wireless-client-oper` YANG model
4. **SSH CLI**: Interactive shell that parses commands and returns formatted output matching real WLC CLI
5. **Access logging**: All RESTCONF requests and SSH sessions/commands are recorded in a shared log store
6. **Dashboard**: Serves an embedded web UI on a separate HTTP port; live log updates via Server-Sent Events (SSE)

### Adding a New Device

Just add another entry under `devices:` in the YAML, then restart with `-setup-ips`.
