# CLAUDE.md

## Project Overview

Cisco 9800-CL WLC Simulator — a Go application that simulates multiple Cisco Catalyst 9800-CL Wireless LAN Controllers for testing NMS/monitoring tools.

## Build & Run

```bash
go build -o wlcsim ./cmd/wlcsim/
sudo ./wlcsim -config configs/devices.yaml           # local mode
sudo ./wlcsim -lan -config configs/devices.yaml       # LAN mode
```

Requires sudo for privileged ports (22, 161, 443, 69) and IP alias management.

## Project Structure

- `cmd/wlcsim/main.go` — entry point, flags, signal handling
- `internal/simulator/` — thread-safe device lifecycle manager (Simulator struct)
- `internal/config/` — YAML config + template loading
- `internal/device/` — Device/AP/Client models, running-config template rendering
- `internal/restconf/` — HTTPS RESTCONF server (XML/JSON, client-oper-data + AP oper data)
- `internal/sshsim/` — SSH server with IOS-XE CLI, SCP/SFTP, interactive copy dialogs
- `internal/snmp/` — SNMPv2c agent (GoSNMPServer library)
- `internal/tftpsim/` — on-demand TFTP server with idle timeout
- `internal/dashboard/` — web dashboard (embedded HTML/JS/CSS), REST API, SSE
- `internal/network/` — IP alias management, interface detection, ARP probing
- `internal/accesslog/` — shared access log store with pub/sub
- `configs/devices.yaml` — sample device config
- `configs/running-config.tmpl` — IOS-XE config template (Go text/template)

## Key Patterns

- **One goroutine per protocol per device**: RESTCONF, SSH, SNMP each get their own goroutine
- **Simulator struct** (`internal/simulator/`): owns the device list with `sync.RWMutex`, all mutations go through it
- **Access logging**: all protocols write to shared `accesslog.Store`, dashboard streams via SSE
- **Config template**: `device.InitConfig(tmplText)` renders and caches; `RunningConfig()` returns cached string (lazy init if empty)
- **TFTP on-demand**: `tftpsim.Manager.EnsureRunning()` blocks until server is listening, 30s idle shutdown
- **Content negotiation**: RESTCONF checks `Accept` header, defaults to XML, JSON when `application/yang-data+json`

## Testing

No test suite. Manual testing workflow:

```bash
# RESTCONF
curl -sk -u admin:Cisco123 -H "Accept: application/yang-data+json" \
  https://10.99.0.1/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data

# SSH
sshpass -p Cisco123 ssh -o StrictHostKeyChecking=no admin@10.99.0.1 "show version"

# SNMP
snmpwalk -v2c -c public 10.99.0.1 1.3.6.1.2.1.1

# SCP
scp -O admin@10.99.0.1:running-config ./test.txt

# Dashboard API
curl http://localhost:8080/api/devices
curl -X POST http://localhost:8080/api/devices -d '{"hostname":"NEW","ip":"10.99.0.3"}'
```

## Dependencies

- `golang.org/x/crypto/ssh` — SSH server
- `github.com/slayercat/GoSNMPServer` + `github.com/gosnmp/gosnmp` — SNMP agent
- `github.com/pkg/sftp` — SFTP subsystem
- `github.com/pin/tftp/v3` — TFTP server and client
- `github.com/mdlayher/arp` — ARP probing and gratuitous ARP
- `gopkg.in/yaml.v3` — YAML config parsing

## Important Notes

- Build warnings from `github.com/shoenig/go-m1cpu` (transitive dep) are harmless
- macOS: local ping to LAN-mode aliases doesn't work (kernel quirk); other machines work fine
- SNMP community string validation requires `SecurityConfig.NoSecurity: false` (not `true`)
- Config template uses Go `text/template` syntax with `.Hostname`, `.IP`, `.Version`, `.Serial`, `.VLANs`, `.WLANs`, `.APs`
