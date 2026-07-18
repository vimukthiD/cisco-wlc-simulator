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
- **Config Persistence** — runtime changes survive restarts; export/import backups and reinitialize (factory reset or clear) from the dashboard
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
- **Config toolbar** — Export (download a YAML backup), Import (restore/replace from a file), Factory Reset (restore seed), Clear All (empty)

### Config Persistence

Changes made at runtime (via the dashboard or REST API) are saved to a **state file** — `state.yaml` next to the config by default (override with `-state`) — in the same format as `devices.yaml`. On startup the simulator restores this state if present, so nothing is lost across restarts. Deleting the state file (or clicking **Factory Reset**) reverts to the bundled seed config. The header toolbar also lets you **Export** the current state as a portable backup and **Import** one to replace everything.

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
| GET | `/api/config/export` | Download current state as `devices.yaml` |
| POST | `/api/config/import` | Replace all state from a posted YAML body |
| POST | `/api/config/reset?mode=factory` | Restore the bundled seed config |
| POST | `/api/config/reset?mode=clear` | Remove all devices (empty) |
| GET | `/api/auth` | Get credentials |
| GET | `/api/system` | System metrics (CPU, memory, uptime) |
| GET | `/api/logs` | Recent access log entries |
| GET | `/api/logs/stream` | SSE stream of new log entries |
| GET | `/api/update/status` | Running version + cached update availability |
| POST | `/api/update/check` | Check GitHub for a newer release (appliance only) |
| POST | `/api/update/apply` | Download, verify, install and restart (appliance only) |

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
| `-config` | `configs/devices.yaml` | Path to the seed devices config file |
| `-state` | (next to `-config`) | Path to the persisted runtime state file (`state.yaml`) |
| `-dashboard-port` | `8080` | Web dashboard HTTP port |
| `-lan` | `false` | LAN mode — bind to physical interface for network accessibility |
| `-interface` | (auto-detect) | Network interface for LAN mode |
| `-setup-ips` | `false` | Only add IP aliases, then exit |
| `-teardown-ips` | `false` | Only remove IP aliases, then exit |

## VM Appliance (OVA)

Pre-built VM images are available for deploying the simulator as a standalone virtual appliance. The VM boots directly into LAN mode with a console status display.

Pre-built binaries (Linux/macOS, amd64/arm64) and the AMD64 OVA are published to the GitHub Releases page on every `v*` tag via `.github/workflows/release.yml`. Build locally only if you need an ARM64 OVA or a custom image.

### Verifying Releases

Every release ships a `checksums.txt` (SHA256 of every artifact) and a `checksums.txt.minisig` ([minisign](https://jedisct1.github.io/minisign/) Ed25519 signature). The signing public key is committed at [`ova/keys/wlcsim.pub`](ova/keys/wlcsim.pub).

```bash
# Download artifact + checksums + signature from the release
curl -LO https://github.com/<owner>/cisco-wlc-simulator/releases/download/vX.Y.Z/wlcsim-linux-amd64
curl -LO https://github.com/<owner>/cisco-wlc-simulator/releases/download/vX.Y.Z/checksums.txt
curl -LO https://github.com/<owner>/cisco-wlc-simulator/releases/download/vX.Y.Z/checksums.txt.minisig

# Verify the signature, then verify the binary against the signed checksum
minisign -V -p ova/keys/wlcsim.pub -m checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

Maintainer signing setup and key-rotation procedure live in [`ova/keys/README.md`](ova/keys/README.md).

### Building OVA Images

Requires: Go 1.23+, [Packer](https://www.packer.io/), [QEMU](https://www.qemu.org/)

```bash
brew install packer qemu    # macOS

make ova-arm64              # ARM64 (Apple Silicon, UTM)
make ova-amd64              # AMD64 (Intel, VMware, VirtualBox)
make ova-all                # Both architectures
```

Output: `build/wlcsim-arm64.ova` (~70MB), `build/wlcsim-amd64.ova`

### What's Inside

- **Alpine Linux 3.21** with `linux-lts` kernel — broad hypervisor support (QEMU/KVM, VMware Fusion ARM with NVMe + vmxnet3, VirtualBox)
- **NVMe in initramfs**: root mountable on VMware Fusion ARM out of the box
- **Auto-start**: simulator launches in LAN mode on boot via OpenRC (`/etc/init.d/wlcsim`)
- **Console TUI**: VM console (`wlcsim-console`) shows device list, IPs, dashboard URL, recent activity; `r`/`s`/`q` for reboot/shutdown/shell
- **Networking**: DHCP on eth0, bridged to host network
- **Dashboard**: accessible at `http://<vm-ip>:8080`
- **All protocols**: SSH (22), HTTPS (443), SNMP (161) on auto-assigned LAN IPs
- **Self-update**: check GitHub for a newer release and update in place from the dashboard, with automatic rollback if the new build fails to start (`ca-certificates` bundled for TLS)

### Deploying

1. Import the OVA into VMware, VirtualBox, or UTM
2. Set the network adapter to **Bridged** mode
3. Boot the VM
4. The console displays assigned IPs and dashboard URL
5. Access devices from any machine on the LAN

### System Update (in-place)

The appliance updates itself in place — no need to re-import a new OVA. From the dashboard's **System Update** card:

1. It shows the running version with a **Check for updates** button.
2. Check queries the latest GitHub release (`releases/latest`).
3. If a newer stable release exists, **Update Now** downloads the new `wlcsim` + `wlcsim-console` binaries, verifies them against the release's `checksums.txt` (SHA-256), swaps them into `/usr/local/bin` (backing the old ones up to `.bak`), and restarts the service.
4. A detached helper — the *pre-swap* binary re-executed via `/proc/self/exe`, so trusted old code performs the cut-over — health-checks the restart and **automatically rolls back** to the `.bak` binaries if the new version doesn't come up. The outcome ("Updated to …" / "Rolled back to …") is shown after the page reloads. Details are logged to `/var/log/wlcsim-update.log`.

The feature is **appliance-only** (gated on the presence of `/etc/init.d/wlcsim`); on a native `./wlcsim` run the card shows the version but no update controls. It needs outbound HTTPS to `api.github.com` and GitHub's release CDN. Only releases published *after* an appliance was built are offered — an appliance can only self-update if its own binary already contains the updater.

### Testing the Update Flow on a VM

The download → verify → swap → restart → rollback path only runs on a real Linux/OpenRC appliance. Two ways to exercise it end to end:

#### Option A — local mock release server (self-contained, tests rollback)

No public release needed. A build tag (`updatetest`) enables a `WLCSIM_UPDATE_API_BASE` override that points the updater at a local mock server. **This tag is never set by the default or release targets, so it can't reach a production build.**

1. **Build a test OVA** with the mock hook (match your host arch):

   ```bash
   make ova-arm64 GO_TAGS=updatetest      # or: make ova-amd64 GO_TAGS=updatetest
   ```

2. **Serve a "new release"** from your host — build newer-versioned binaries and run the mock (auto-generates `checksums.txt`):

   ```bash
   mkdir newrelease
   GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=v9.9.9" -o newrelease/wlcsim-linux-arm64 ./cmd/wlcsim
   GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=v9.9.9" -o newrelease/wlcsim-console-linux-arm64 ./cmd/wlcsim-console
   go run ./hack/mockrelease -dir ./newrelease -tag v9.9.9 -addr :8099
   ```

   (Use `-linux-amd64` names if your OVA is amd64.)

3. **Boot the test OVA** (bridged networking) and point its updater at your host. From the VM console press `q` for a shell:

   ```bash
   # add near the top of the service script so the daemon inherits it
   sed -i '2i export WLCSIM_UPDATE_API_BASE=http://<your-host-ip>:8099' /etc/init.d/wlcsim
   rc-service wlcsim restart
   ```

4. **Update**: open `http://<vm-ip>:8080` → **System Update** card → **Check for updates** (shows `v9.9.9`) → **Update Now**. Watch the log panel; the page reloads on the new version.

5. **Test rollback**: serve a "new" binary that never starts, under a higher tag, then Update again — the appliance installs it, the health check fails, and it auto-rolls-back:

   ```bash
   printf '#!/bin/sh\nexec sleep infinity\n' > newrelease/wlcsim-linux-arm64
   chmod +x newrelease/wlcsim-linux-arm64
   go run ./hack/mockrelease -dir ./newrelease -tag v9.9.10 -addr :8099
   ```

   The card shows "Rolled back to …"; `/var/log/wlcsim-update.log` on the VM has the full sequence.

#### Option B — real GitHub releases (public, fully end-to-end)

1. Tag and push two releases (CI builds binaries, console binaries, checksums, and the AMD64 OVA on each `v*` tag):

   ```bash
   git tag v0.0.10 && git push origin v0.0.10   # deploy this OVA
   git tag v0.0.11 && git push origin v0.0.11   # the target to update to
   ```

2. Deploy the `v0.0.10` OVA (amd64 host for the published OVA; for arm64, `make ova-arm64` at the `v0.0.10` commit — the arm64 *binaries* are still published for it to fetch).
3. On the VM dashboard: **Check for updates** → `v0.0.11` → **Update Now** → confirm the version becomes `v0.0.11` and devices return.
4. Delete the throwaway tags/releases afterward. Note: `releases/latest` ignores pre-releases, so test releases must be full releases.

> For a quick logic check without a VM: `go test ./internal/updater/`.

### Makefile Targets

```
make build              Build native binaries
make run                Run simulator (local mode, requires sudo)
make run-lan            Run simulator (LAN mode, requires sudo)
make build-linux-all    Cross-compile for Linux (amd64 + arm64)
make ova-amd64          Build AMD64 OVA
make ova-arm64          Build ARM64 OVA
make ova-all            Build both OVAs
make clean              Remove build artifacts

# Append GO_TAGS=updatetest to any build/ova target to enable the updater's
# mock-server test hook (WLCSIM_UPDATE_API_BASE). Never used by release builds.
```

## Architecture

```
cmd/
  wlcsim/main.go                 — Entry point, flags, signal handling, auto IP lifecycle
  wlcsim-console/main.go         — Console TUI for VM appliance (ANSI status display)
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
  updater/                       — Appliance-only in-place system update (GitHub check,
                                   download+verify, detached self-exec restart w/ rollback)
  network/
    setup.go                     — IP alias management (loopback + physical interface)
    detect.go                    — Primary interface detection (macOS/Linux)
    arp.go                       — ARP probing, unused IP discovery, gratuitous ARP
configs/
  devices.yaml                   — Sample config with 2 WLCs, 4 APs, 7 clients
  running-config.tmpl            — Customizable Cisco IOS-XE config template
ova/
  packer/wlcsim.pkr.hcl          — Packer template (QEMU builder, ARM64/AMD64)
  packer/setup.sh                — Post-install provisioning script
  scripts/package-ova.sh         — qcow2 → VMDK → OVA packaging
  templates/wlcsim.ovf.tmpl      — OVF descriptor template
  rootfs/                        — Alpine rootfs overlay (service, config, networking)
Makefile                         — Build, run, and OVA targets
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
