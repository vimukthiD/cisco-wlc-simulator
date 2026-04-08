# Cisco 9800-CL WLC Simulator

A lightweight simulator for Cisco Catalyst 9800-CL Wireless LAN Controllers. Simulates multiple WLC devices, each with its own IP address, RESTCONF API, and SSH CLI.

Inspired by [simsnmp](https://github.com/lfbayer/simsnmp) — one IP per simulated device, data-driven configuration.

## Features

- **RESTCONF API** — `Cisco-IOS-XE-wireless-client-oper:client-oper-data` with all sub-endpoints (common-oper-data, dot11-oper-data, traffic-stats, sisf-db-mac, dc-info, policy-data)
- **SSH CLI** — Cisco IOS-XE style shell with `show` commands
- **Multiple Devices** — each device gets its own IP, HTTPS port, and SSH port
- **Virtual IPs** — loopback aliases so each device is individually pingable
- **YAML Config** — define devices, APs, and clients in a single config file

## Quick Start

```bash
# Build
go build -o wlcsim ./cmd/wlcsim/

# Set up virtual IPs (requires sudo)
sudo ./wlcsim -setup-ips -config configs/devices.yaml

# Run the simulator
./wlcsim -config configs/devices.yaml
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

```bash
# Full client operational data
curl -sk -u admin:Cisco123 \
  https://10.99.0.1:9443/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# Just wireless client common data
curl -sk -u admin:Cisco123 \
  https://10.99.0.1:9443/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/common-oper-data

# Traffic stats
curl -sk -u admin:Cisco123 \
  https://10.99.0.1:9443/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/traffic-stats

# Second device
curl -sk -u admin:Cisco123 \
  https://10.99.0.2:9444/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data
```

### SSH

```bash
ssh -p 2221 admin@10.99.0.1
# Password: Cisco123

# Available CLI commands:
#   show version
#   show wireless client summary
#   show wireless client mac-address <mac>
#   show ip interface brief
#   show ap summary
#   show wlan summary
```

## Configuration

All devices are defined in `configs/devices.yaml`:

```yaml
auth:
  username: admin
  password: Cisco123

devices:
  - hostname: WLC-SITE-A
    ip: 10.99.0.1
    https_port: 9443
    ssh_port: 2221
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

See [configs/devices.yaml](configs/devices.yaml) for a full example with two devices.

## Architecture

```
cmd/wlcsim/main.go        — Entry point, flag parsing, signal handling
internal/
  config/config.go         — YAML config loading with defaults
  device/device.go         — Device, AP, Client data models
  restconf/
    server.go              — HTTPS server with self-signed TLS, basic auth
    handlers.go            — RESTCONF endpoint handlers (YANG-format JSON)
  sshsim/server.go         — SSH server with Cisco IOS-XE CLI simulation
  network/setup.go         — Virtual IP management (macOS/Linux)
configs/
  devices.yaml             — Sample config with 2 WLCs, 4 APs, 7 clients
```

### How It Works

1. **IP Setup** (`-setup-ips`): Adds loopback aliases via `ifconfig lo0 alias` (macOS) or `ip addr add` (Linux)
2. **Per-device servers**: Each device spawns its own HTTPS and SSH server, bound to its specific IP
3. **RESTCONF responses**: Built from the YAML config, formatted to match the real `Cisco-IOS-XE-wireless-client-oper` YANG model
4. **SSH CLI**: Interactive shell that parses commands and returns formatted output matching real WLC CLI

### Adding a New Device

Just add another entry under `devices:` in the YAML, then restart with `-setup-ips`.
