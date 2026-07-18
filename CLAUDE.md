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
- `internal/updater/` — appliance-only in-place system update (GitHub release check, download+verify, detached self-exec restart with rollback)
- `configs/devices.yaml` — sample device config (2 WLCs, 4 APs, 7 clients)
- `configs/running-config.tmpl` — IOS-XE config template (Go text/template)
- `ova/` — VM appliance build system (Packer, Alpine rootfs, OVA packaging)
- `.github/workflows/release.yml` — tag-driven release CI (cross-built binaries + AMD64 OVA)

## Key Patterns

- **One goroutine per protocol per device**: RESTCONF, SSH, SNMP each get their own goroutine
- **Simulator struct** (`internal/simulator/`): owns the device list with `sync.RWMutex`, all mutations go through it
- **Access logging**: all protocols write to shared `accesslog.Store`, dashboard streams via SSE
- **Config template**: `device.InitConfig(tmplText)` renders and caches; `RunningConfig()` returns cached string (lazy init if empty)
- **TFTP on-demand**: `tftpsim.Manager.EnsureRunning()` blocks until server is listening, 30s idle shutdown
- **Content negotiation**: RESTCONF checks `Accept` header, defaults to XML, JSON when `application/yang-data+json`
- **LAN mode**: IPs added to physical interface + loopback (macOS dual alias), gratuitous ARP, auto-assign from subnet
- **AP SSIDs**: APs have explicit SSID list; clients can only join AP's SSIDs; auto-populated from client data on load
- **Default AP**: a site is itself an access point — every device auto-gets one locked AP named after its hostname, marked `default: true` (MAC derived from the hostname). `device.EnsureDefaultAP()` is idempotent and runs on every create/load path (seed via `config.applyDefaults`, runtime via `simulator.AddDevice`, import via `ParseYAML`); on import it adopts an existing same-named AP instead of duplicating. The default AP can't be removed (`RemoveAP` rejects it) or renamed, but is otherwise editable (SSIDs, clients, model, MAC).
- **Persistence**: every simulator mutation writes the full state to a YAML state file (atomic temp+rename) in the same `devices.yaml` format. On startup, a present state file is loaded in preference to the seed config; its **absence** means "use the seed." `state.yaml` lives next to `-config` by default (override with `-state`).
- **State file invariant**: the state file always mirrors the running state. Factory Reset re-applies the seed and *deletes* the state file (restart → seed); Clear All empties the devices and *writes* an empty state; Import replaces everything and writes the state. `internal/config` owns both directions of the YAML↔`Config` mapping (`Load`/`ParseYAML` in, `Marshal`/`Save` out).
- **Graceful shutdown**: each protocol `Serve` takes a `stop <-chan struct{}`; the simulator holds a per-device `deviceHandle` (stop channel + WaitGroup) so it can stop a device's three servers and reuse its `IP:port` immediately (required for reset/import; also fixes `RemoveDevice`'s old socket leak).
- **In-place system update** (`internal/updater`, appliance-only): the build version is baked in via `-X main.version` (declared as `var version` in `cmd/wlcsim/main.go`). `updater.New` gates the feature on the appliance (presence of `/etc/init.d/wlcsim`, or `WLCSIM_APPLIANCE=1` for local testing). The dashboard exposes `GET /api/update/status`, `POST /api/update/check` (queries GitHub `releases/latest`), and `POST /api/update/apply`. Apply downloads `wlcsim-linux-<arch>` + `wlcsim-console-linux-<arch>` + `checksums.txt` to `/var/lib/wlcsim/update`, SHA-256-verifies them (nothing under `/usr/local/bin` is touched until verified), then hands off to a **detached helper** — the *current, pre-swap* binary re-executed via `/proc/self/exe -update-helper` so trusted old code performs the swap. The helper: confirms the old process actually stopped, backs up live binaries to `.bak`, installs the new ones, `rc-service wlcsim restart`, health-checks `:8080/api/system`, and **auto-rolls-back to `.bak` on failure** — reporting an outcome grounded in a real post-recovery health check (never optimistic). Progress streams to the live log panel as `accesslog.Entry{Type:"system"}`; the outcome is persisted to `/var/lib/wlcsim/update/last-result.json` and surfaced in `Status` so the post-restart page shows "Updated"/"Rolled back". Helper output goes to `/var/log/wlcsim-update.log`. Only benefits appliances built from the release that ships the updater onward.

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

# Config persistence: export / import / reinitialize
curl http://localhost:8080/api/config/export -o backup.yaml          # download current state
curl -X POST http://localhost:8080/api/config/import --data-binary @backup.yaml   # replace all state
curl -X POST 'http://localhost:8080/api/config/reset?mode=factory'   # restore seed devices.yaml
curl -X POST 'http://localhost:8080/api/config/reset?mode=clear'     # remove all devices

# System update (appliance-only; run local dashboard with WLCSIM_APPLIANCE=1 to exercise)
curl http://localhost:8080/api/update/status                         # version + cached availability (no network)
curl -X POST http://localhost:8080/api/update/check                  # query GitHub releases/latest
curl -X POST http://localhost:8080/api/update/apply                  # download+verify+install+restart (appliance only)
```

### Testing the updater

- **Unit tests** (pure logic + network/verify parsing, no real GitHub): `go test ./internal/updater/`. Covers version comparison, `releases/latest` decoding, checksum parsing, and download+SHA-256 verification via `httptest`.
- **End-to-end against a mock release** (no public release needed): the `updatetest` build tag enables a `WLCSIM_UPDATE_API_BASE` override. It is **opt-in and never set by default or release targets**, so production binaries/OVAs never contain it.
  1. Build a test binary/OVA with the hook: `make build GO_TAGS=updatetest` (or `make ova-arm64 GO_TAGS=updatetest`).
  2. Put the "new" `wlcsim-linux-<arch>` + `wlcsim-console-linux-<arch>` in a dir and serve them: `go run ./hack/mockrelease -dir ./newrelease -tag v9.9.9 -addr :8099` (auto-generates `checksums.txt`).
  3. Give the appliance's `wlcsim` service `WLCSIM_UPDATE_API_BASE=http://<host>:8099` (e.g. add `export ...` to `/etc/init.d/wlcsim`), then Check → Update. Drop a deliberately-broken `wlcsim-linux-<arch>` (one that won't serve `:8080`) in the dir to exercise the auto-rollback path.
- **Isolated helper file-op dry-run**: stub `rc-service` on `PATH`, hand-write a `helper.json`, run `wlcsim -update-helper /path/helper.json` — validates the backup/swap/rollback file moves without GitHub or a full OVA.

## OVA Build

The OVA build uses Packer with QEMU to create an Alpine Linux VM appliance:

1. `make ova-arm64` cross-compiles Go binaries, then Packer boots Alpine ISO in QEMU
2. Boot commands set up SSH on the live ISO
3. SSH provisioner runs `setup-*` commands to install Alpine to disk
4. After reboot, file provisioners upload binaries and configs
5. Setup script installs packages, enables services, configures console TUI
6. Post-processing converts qcow2 → streamOptimized VMDK → OVA

**Prerequisites**: Go 1.23+, Packer (`brew install packer`), QEMU (`brew install qemu`)

**Key files**:
- `ova/packer/wlcsim.pkr.hcl` — Packer template (QEMU builder, ARM64/AMD64, Alpine 3.21)
- `ova/packer/setup.sh` — post-install provisioning (LTS kernel swap, NVMe initramfs, packages, service, console)
- `ova/rootfs/etc/` — Alpine overlay (init.d service, network config, motd, sample configs)
- `ova/scripts/package-ova.sh` — VMDK conversion + OVF + manifest + tar
- `ova/templates/wlcsim.ovf.tmpl` — OVF descriptor (2 vCPU, 256MB RAM, bridged NIC)
- `.github/workflows/release.yml` — tag-triggered CI: builds Linux/Darwin binaries + AMD64 OVA, publishes GitHub Release

**Kernel choice**: The build installs `linux-lts` over Alpine's default `linux-virt` so the same image boots on QEMU/KVM, VMware Fusion ARM (NVMe + vmxnet3), and VirtualBox. Bootloader configs (GRUB/extlinux) are rewritten to point at the LTS kernel and NVMe is added to the initramfs.

**macOS EFI firmware**: ARM64 builds need `/opt/homebrew/share/qemu/edk2-aarch64-code.fd` (installed by `brew install qemu`)

## Dependencies

- `golang.org/x/crypto/ssh` — SSH server
- `github.com/slayercat/GoSNMPServer` + `github.com/gosnmp/gosnmp` — SNMP agent
- `github.com/pkg/sftp` — SFTP subsystem
- `github.com/pin/tftp/v3` — TFTP server and client
- `github.com/mdlayher/arp` — ARP probing and gratuitous ARP
- `gopkg.in/yaml.v3` — YAML config parsing

## Important Notes

- **State file**: defaults to `state.yaml` beside `-config` (so `configs/state.yaml` locally, `/etc/wlcsim/state.yaml` on the appliance — both writable). It's gitignored. Delete it (or Factory Reset) to revert to the seed. The LAN reassigned IPs are captured on the first mutation, so restarts keep them.
- Build warnings from `github.com/shoenig/go-m1cpu` (transitive dep) are harmless
- macOS: local ping to LAN-mode aliases requires dual alias (en0 + lo0), already handled
- SNMP community string validation requires `SecurityConfig.NoSecurity: false` (not `true`)
- Config template uses Go `text/template` syntax with `.Hostname`, `.IP`, `.Version`, `.Serial`, `.VLANs`, `.WLANs`, `.APs`
- Packer on macOS: QEMU doesn't support GTK display; use `headless = true` (VNC) or `display = "cocoa"`
- Alpine `arping` is in the `iputils` package, not `arping`
- **System update needs outbound internet**: the appliance's update check/apply reaches `api.github.com` and GitHub release-asset hosts over HTTPS. `setup.sh` installs `ca-certificates` so Go's static binary can verify TLS; without egress the check fails gracefully (surfaced as an error, service untouched). `release.yml` publishes `wlcsim-console-linux-{amd64,arm64}` alongside the main binaries so both can be updated in place.
- **Update version wiring**: `-X main.version` targets `var version` in `cmd/wlcsim/main.go` (previously the ldflag was inert — no such var existed). Tagged CI builds bake in the tag (`v0.0.9`); local `make build` bakes in `git describe`; a bare `go build` leaves it `"dev"` (always sees an update available).
