# CLAUDE.md

## Project Overview

Cisco 9800-CL WLC Simulator — a Go application that simulates multiple Cisco Catalyst 9800-CL Wireless LAN Controllers for testing NMS/monitoring tools. Deployable as a native binary or as a VM appliance (OVA).

## Build & Run

```bash
# Native
go build -o wlcsim ./cmd/wlcsim/
sudo ./wlcsim -config configs/devices.yaml           # local mode
sudo ./wlcsim -lan -config configs/devices.yaml       # LAN mode

# Or use Makefile
make build          # build native binaries
make run            # run local mode
make run-lan        # run LAN mode

# OVA (requires Packer + QEMU)
make ova-arm64      # ARM64 OVA (~70MB)
make ova-amd64      # AMD64 OVA
```

Requires sudo for privileged ports (22, 161, 443, 69) and IP alias management.

## Project Structure

- `cmd/wlcsim/` — main simulator binary (entry point, flags, signal handling)
- `cmd/wlcsim-console/` — console TUI for VM appliance (ANSI status display)
- `internal/simulator/` — thread-safe device lifecycle manager (Simulator struct with RWMutex)
- `internal/config/` — YAML config + template loading
- `internal/device/` — Device/AP/Client models, running-config template rendering
- `internal/restconf/` — HTTPS RESTCONF server (XML/JSON, client-oper-data + AP oper data)
- `internal/sshsim/` — SSH server with IOS-XE CLI, SCP/SFTP, interactive copy dialogs
- `internal/snmp/` — SNMPv2c agent (GoSNMPServer library, 30+ OIDs)
- `internal/tftpsim/` — on-demand TFTP server with idle timeout and SO_REUSEPORT
- `internal/dashboard/` — web dashboard (embedded HTML/JS/CSS), REST API, SSE, CPU sampler
- `internal/network/` — IP alias management, interface detection, ARP probing
- `internal/accesslog/` — shared access log store with pub/sub
- `configs/devices.yaml` — sample device config (2 WLCs, 4 APs, 7 clients)
- `configs/running-config.tmpl` — IOS-XE config template (Go text/template)
- `ova/` — VM appliance build system (Packer, Alpine rootfs, OVA packaging)

## Key Patterns

- **One goroutine per protocol per device**: RESTCONF, SSH, SNMP each get their own goroutine
- **Simulator struct** (`internal/simulator/`): owns the device list with `sync.RWMutex`, all mutations go through it
- **Access logging**: all protocols write to shared `accesslog.Store`, dashboard streams via SSE
- **Config template**: `device.InitConfig(tmplText)` renders and caches; `RunningConfig()` returns cached string (lazy init if empty)
- **TFTP on-demand**: `tftpsim.Manager.EnsureRunning()` blocks until server is listening, 30s idle shutdown
- **Content negotiation**: RESTCONF checks `Accept` header, defaults to XML, JSON when `application/yang-data+json`
- **LAN mode**: IPs added to physical interface + loopback (macOS dual alias), gratuitous ARP, auto-assign from subnet
- **AP SSIDs**: APs have explicit SSID list; clients can only join AP's SSIDs; auto-populated from client data on load

## Testing

No test suite. Manual testing workflow:

```bash
# RESTCONF
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# AP operational data
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-access-point-oper:access-point-oper-data

# SSH
sshpass -p Cisco123 ssh -o StrictHostKeyChecking=no admin@10.99.0.1 "show version"

# SNMP
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.1

# SCP
scp -O admin@10.99.0.1:running-config ./test.txt

# Dashboard API
curl http://localhost:8080/api/devices
curl -X POST http://localhost:8080/api/devices -d '{"hostname":"NEW","ip":"10.99.0.3"}'
curl -X POST http://localhost:8080/api/devices/ap -d '{"device_ip":"10.99.0.1","ap":{"name":"AP-1","mac":"00:aa:bb:cc:dd:00","ssids":["WiFi"]}}'
curl -X PUT http://localhost:8080/api/devices/client/move -d '{"device_ip":"10.99.0.1","client_mac":"aa:bb:cc:11:22:01","new_ap":"AP-2","new_ssid":"Guest"}'
```

## OVA Build

The OVA build uses Packer with QEMU to create an Alpine Linux VM appliance:

1. `make ova-arm64` cross-compiles Go binaries, then Packer boots Alpine ISO in QEMU
2. Boot commands set up SSH on the live ISO
3. SSH provisioner runs `setup-*` commands to install Alpine to disk
4. After reboot, file provisioners upload binaries and configs
5. Setup script installs packages, enables services, configures console TUI
6. Post-processing converts qcow2 → streamOptimized VMDK → OVA

**Prerequisites**: Go 1.21+, Packer (`brew install packer`), QEMU (`brew install qemu`)

**Key files**:
- `ova/packer/wlcsim.pkr.hcl` — Packer template (QEMU builder, ARM64/AMD64)
- `ova/packer/setup.sh` — post-install provisioning (packages, service, console)
- `ova/scripts/package-ova.sh` — VMDK conversion + OVF + manifest + tar
- `ova/templates/wlcsim.ovf.tmpl` — OVF descriptor (2 vCPU, 256MB RAM, bridged NIC)

**macOS EFI firmware**: ARM64 builds need `/opt/homebrew/share/qemu/edk2-aarch64-code.fd` (installed by `brew install qemu`)

## Dependencies

- `golang.org/x/crypto/ssh` — SSH server
- `github.com/slayercat/GoSNMPServer` + `github.com/gosnmp/gosnmp` — SNMP agent
- `github.com/pkg/sftp` — SFTP subsystem
- `github.com/pin/tftp/v3` — TFTP server and client
- `github.com/mdlayher/arp` — ARP probing and gratuitous ARP
- `gopkg.in/yaml.v3` — YAML config parsing

## Important Notes

- Build warnings from `github.com/shoenig/go-m1cpu` (transitive dep) are harmless
- macOS: local ping to LAN-mode aliases requires dual alias (en0 + lo0), already handled
- SNMP community string validation requires `SecurityConfig.NoSecurity: false` (not `true`)
- Config template uses Go `text/template` syntax with `.Hostname`, `.IP`, `.Version`, `.Serial`, `.VLANs`, `.WLANs`, `.APs`
- Packer on macOS: QEMU doesn't support GTK display; use `headless = true` (VNC) or `display = "cocoa"`
- Alpine `arping` is in the `iputils` package, not `arping`
