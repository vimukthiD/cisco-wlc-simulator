package simulator

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/accesslog"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/config"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/device"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/network"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/restconf"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/snmp"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/sshsim"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/tftpsim"
)

// deviceHandle owns the lifecycle of one device's three protocol servers.
// Closing stop tells each Serve loop to shut down; wg completes once all
// three have returned and released their listeners.
type deviceHandle struct {
	stop chan struct{}
	wg   sync.WaitGroup
}

// Options configures persistence and networking behavior for a Simulator.
type Options struct {
	StatePath string // where runtime state is saved ("" disables persistence)
	SeedPath  string // seed devices.yaml, used by Factory Reset
	LAN       bool   // true when devices are bound to a physical interface
	Iface     string // resolved LAN interface name (LAN mode only)
}

// Simulator manages the lifecycle of simulated WLC devices.
// It is safe for concurrent use.
type Simulator struct {
	mu       sync.RWMutex
	devices  []*device.Device
	handles  map[string]*deviceHandle // keyed by device IP
	auth     config.Auth
	logs     *accesslog.Store
	tftpMgr  *tftpsim.Manager
	tmplText string

	statePath string
	seedPath  string
	lan       bool
	iface     string
}

// New creates a Simulator from the loaded config.
func New(cfg *config.Config, logs *accesslog.Store, tmplText string, opts Options) *Simulator {
	sim := &Simulator{
		handles:   map[string]*deviceHandle{},
		auth:      cfg.Auth,
		logs:      logs,
		tftpMgr:   tftpsim.NewManager(logs),
		tmplText:  tmplText,
		statePath: opts.StatePath,
		seedPath:  opts.SeedPath,
		lan:       opts.LAN,
		iface:     opts.Iface,
	}
	for i := range cfg.Devices {
		sim.devices = append(sim.devices, &cfg.Devices[i])
	}
	return sim
}

// Devices returns a snapshot of all current devices.
func (s *Simulator) Devices() []device.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]device.Device, len(s.devices))
	for i, d := range s.devices {
		result[i] = *d
	}
	return result
}

// Auth returns the shared auth config.
func (s *Simulator) Auth() config.Auth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auth
}

// StartAll starts servers for all configured devices.
func (s *Simulator) StartAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dev := range s.devices {
		s.handles[dev.IP] = s.startDeviceServers(dev)
	}
}

// AddDevice validates, sets up the IP alias, initializes config, starts
// all servers, and adds the device to the list. Returns error on failure.
func (s *Simulator) AddDevice(dev device.Device) error {
	// Apply defaults
	if dev.HTTPSPort == 0 {
		dev.HTTPSPort = 443
	}
	if dev.SSHPort == 0 {
		dev.SSHPort = 22
	}
	if dev.SNMPPort == 0 {
		dev.SNMPPort = 161
	}
	if dev.Model == "" {
		dev.Model = "C9800-CL-K9"
	}
	if dev.Version == "" {
		dev.Version = "17.12.1"
	}
	if dev.Hostname == "" {
		return fmt.Errorf("hostname is required")
	}
	if dev.IP == "" {
		return fmt.Errorf("ip is required")
	}

	// Check for duplicate IP
	s.mu.RLock()
	for _, d := range s.devices {
		if d.IP == dev.IP {
			s.mu.RUnlock()
			return fmt.Errorf("device with IP %s already exists", dev.IP)
		}
	}
	s.mu.RUnlock()

	// A site always has its locked default AP (named after the hostname).
	dev.EnsureDefaultAP()

	dev.StartTime = time.Now()
	dev.InitConfig(s.tmplText)

	// Add IP alias
	if err := network.AddIP(dev.IP); err != nil {
		return fmt.Errorf("add IP alias %s: %w", dev.IP, err)
	}

	newDev := &dev
	s.mu.Lock()
	s.devices = append(s.devices, newDev)
	s.handles[newDev.IP] = s.startDeviceServers(newDev)
	s.saveLocked()
	s.mu.Unlock()

	log.Printf("[%s] Device added at runtime: %s (HTTPS:%d, SSH:%d, SNMP:%d)",
		dev.Hostname, dev.IP, dev.HTTPSPort, dev.SSHPort, dev.SNMPPort)
	return nil
}

// RemoveDevice removes a device by IP, stops its servers, and tears down its
// IP alias. Returns the removed device.
func (s *Simulator) RemoveDevice(ip string) error {
	s.mu.Lock()
	found := -1
	for i, d := range s.devices {
		if d.IP == ip {
			found = i
			break
		}
	}
	if found == -1 {
		s.mu.Unlock()
		return fmt.Errorf("device with IP %s not found", ip)
	}
	dev := s.devices[found]
	s.devices = append(s.devices[:found], s.devices[found+1:]...)
	h := s.handles[ip]
	delete(s.handles, ip)
	s.saveLocked()
	s.mu.Unlock()

	// Stop servers (frees the IP:port) then remove the alias. Done outside the
	// lock so the brief wait for goroutines to exit doesn't block reads.
	if h != nil {
		s.stopHandle(h)
	}
	if err := network.RemoveIP(dev.IP); err != nil {
		log.Printf("[%s] Warning: failed to remove IP alias: %v", dev.Hostname, err)
	}

	log.Printf("[%s] Device removed: %s", dev.Hostname, dev.IP)
	return nil
}

// AddAP adds an access point to a device identified by IP.
func (s *Simulator) AddAP(deviceIP string, ap device.AP) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP == deviceIP {
			d.APs = append(d.APs, ap)
			d.InitConfig(s.tmplText)
			s.saveLocked()
			return nil
		}
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// RemoveAP removes an access point from a device by name.
func (s *Simulator) RemoveAP(deviceIP, apName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP == deviceIP {
			for j := range d.APs {
				if d.APs[j].Name == apName {
					if d.APs[j].Default {
						return fmt.Errorf("cannot remove %q: it is the site's default AP", apName)
					}
					d.APs = append(d.APs[:j], d.APs[j+1:]...)
					d.InitConfig(s.tmplText)
					s.saveLocked()
					return nil
				}
			}
			return fmt.Errorf("AP %s not found on device %s", apName, deviceIP)
		}
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// RemoveClient removes a wireless client by MAC from a device.
func (s *Simulator) RemoveClient(deviceIP, clientMAC string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP == deviceIP {
			for j := range d.APs {
				for k := range d.APs[j].Clients {
					if d.APs[j].Clients[k].MAC == clientMAC {
						d.APs[j].Clients = append(d.APs[j].Clients[:k], d.APs[j].Clients[k+1:]...)
						d.InitConfig(s.tmplText)
						s.saveLocked()
						return nil
					}
				}
			}
			return fmt.Errorf("client %s not found on device %s", clientMAC, deviceIP)
		}
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// AddClient adds a wireless client to an AP on a device.
func (s *Simulator) AddClient(deviceIP, apName string, client device.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP == deviceIP {
			for j := range d.APs {
				if d.APs[j].Name == apName {
					d.APs[j].Clients = append(d.APs[j].Clients, client)
					d.InitConfig(s.tmplText)
					s.saveLocked()
					return nil
				}
			}
			return fmt.Errorf("AP %s not found on device %s", apName, deviceIP)
		}
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// UpdateAPSSIDs replaces the SSID list for an AP.
func (s *Simulator) UpdateAPSSIDs(deviceIP, apName string, ssids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP == deviceIP {
			for j := range d.APs {
				if d.APs[j].Name == apName {
					d.APs[j].SSIDs = ssids
					d.InitConfig(s.tmplText)
					s.saveLocked()
					return nil
				}
			}
			return fmt.Errorf("AP %s not found on device %s", apName, deviceIP)
		}
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// UpdateClient updates a client's AP and/or SSID (for simulating roaming).
func (s *Simulator) UpdateClient(deviceIP, clientMAC string, newAPName, newSSID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.IP != deviceIP {
			continue
		}
		// Find and remove client from current AP
		var client *device.Client
		for j := range d.APs {
			for k := range d.APs[j].Clients {
				if d.APs[j].Clients[k].MAC == clientMAC {
					client = &device.Client{}
					*client = d.APs[j].Clients[k]
					d.APs[j].Clients = append(d.APs[j].Clients[:k], d.APs[j].Clients[k+1:]...)
					break
				}
			}
			if client != nil {
				break
			}
		}
		if client == nil {
			return fmt.Errorf("client %s not found on device %s", clientMAC, deviceIP)
		}
		// Update fields
		if newSSID != "" {
			client.SSID = newSSID
		}
		// Add to target AP
		for j := range d.APs {
			if d.APs[j].Name == newAPName {
				d.APs[j].Clients = append(d.APs[j].Clients, *client)
				d.InitConfig(s.tmplText)
				s.saveLocked()
				return nil
			}
		}
		return fmt.Errorf("target AP %s not found on device %s", newAPName, deviceIP)
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

// ---- State: snapshot, export, import, reset ----

// Snapshot returns the current running state as a Config, suitable for
// serialization (export or persistence).
func (s *Simulator) Snapshot() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

// ExportYAML returns the current running state marshaled as devices.yaml.
func (s *Simulator) ExportYAML() ([]byte, error) {
	return config.Marshal(s.Snapshot())
}

// ImportConfig replaces the entire running state with the parsed YAML and
// persists it (Import = Replace all).
func (s *Simulator) ImportConfig(data []byte) error {
	cfg, err := config.ParseYAML(data)
	if err != nil {
		return err
	}
	if err := s.applyConfig(cfg.Auth, cfg.Devices); err != nil {
		return err
	}
	return s.save()
}

// FactoryReset restores the bundled seed devices.yaml and removes the state
// file, so subsequent restarts also load the seed.
func (s *Simulator) FactoryReset() error {
	if s.seedPath == "" {
		return fmt.Errorf("no seed config configured")
	}
	cfg, err := config.Load(s.seedPath)
	if err != nil {
		return fmt.Errorf("load seed config: %w", err)
	}
	if err := s.applyConfig(cfg.Auth, cfg.Devices); err != nil {
		return err
	}
	return s.deleteState()
}

// ClearAll removes every device (keeping credentials) and writes an empty
// state file, so restarts also start empty.
func (s *Simulator) ClearAll() error {
	if err := s.applyConfig(s.Auth(), nil); err != nil {
		return err
	}
	return s.save()
}

// applyConfig atomically replaces the running device set and auth: it stops
// every current device's servers, tears down their IP aliases, then brings up
// the new set using the active network mode (LAN setup may reassign IPs that
// fall outside the detected subnet). Persisting the result is the caller's job.
func (s *Simulator) applyConfig(newAuth config.Auth, newDevs []device.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Tear down the current set.
	for _, h := range s.handles {
		s.stopHandle(h)
	}
	s.teardownIPs(s.devices)
	s.devices = nil
	s.handles = map[string]*deviceHandle{}
	s.auth = newAuth

	if len(newDevs) == 0 {
		return nil
	}

	// Bring up the new set. SetupIPs/SetupLANIPs may mutate newDevs' IPs.
	if err := s.setupIPs(newDevs); err != nil {
		return fmt.Errorf("set up device IPs: %w", err)
	}
	now := time.Now()
	for i := range newDevs {
		d := &newDevs[i]
		d.StartTime = now
		d.InitConfig(s.tmplText)
		s.devices = append(s.devices, d)
		s.handles[d.IP] = s.startDeviceServers(d)
	}
	log.Printf("Applied config: %d device(s) now running", len(s.devices))
	return nil
}

// ---- internal helpers ----

// snapshotLocked builds a Config from the current state. Caller must hold s.mu.
func (s *Simulator) snapshotLocked() *config.Config {
	cfg := &config.Config{Auth: s.auth, TmplText: s.tmplText}
	cfg.Devices = make([]device.Device, len(s.devices))
	for i, d := range s.devices {
		cfg.Devices[i] = *d
	}
	return cfg
}

// save persists the current state to the state file (no lock held during I/O).
func (s *Simulator) save() error {
	if s.statePath == "" {
		return nil
	}
	return config.Save(s.statePath, s.Snapshot())
}

// saveLocked persists while the write lock is already held (used by the
// mutation methods). Errors are logged rather than surfaced to the caller so a
// failed persist never rolls back an applied change.
func (s *Simulator) saveLocked() {
	if s.statePath == "" {
		return
	}
	if err := config.Save(s.statePath, s.snapshotLocked()); err != nil {
		log.Printf("[persist] failed to save state to %s: %v", s.statePath, err)
	}
}

// deleteState removes the state file, if any.
func (s *Simulator) deleteState() error {
	if s.statePath == "" {
		return nil
	}
	if err := os.Remove(s.statePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// setupIPs adds IP aliases for the given devices using the active network mode.
func (s *Simulator) setupIPs(devs []device.Device) error {
	if s.lan {
		_, err := network.SetupLANIPs(devs, s.iface)
		return err
	}
	return network.SetupIPs(devs)
}

// teardownIPs removes IP aliases for the given devices using the active mode.
func (s *Simulator) teardownIPs(devs []*device.Device) {
	vals := make([]device.Device, len(devs))
	for i, d := range devs {
		vals[i] = *d
	}
	if s.lan {
		network.TeardownLANIPs(vals, s.iface)
	} else {
		network.TeardownIPs(vals)
	}
}

// startDeviceServers launches the three protocol servers for a device and
// returns a handle to stop them. Caller must hold s.mu (reads s.auth etc.).
func (s *Simulator) startDeviceServers(dev *device.Device) *deviceHandle {
	auth := s.auth
	logs := s.logs
	tftpMgr := s.tftpMgr
	h := &deviceHandle{stop: make(chan struct{})}
	h.wg.Add(3)
	go func() {
		defer h.wg.Done()
		if err := restconf.Serve(dev, auth, logs, h.stop); err != nil {
			log.Printf("[%s] RESTCONF server error: %v", dev.Hostname, err)
		}
	}()
	go func() {
		defer h.wg.Done()
		if err := sshsim.Serve(dev, auth, logs, tftpMgr, h.stop); err != nil {
			log.Printf("[%s] SSH server error: %v", dev.Hostname, err)
		}
	}()
	go func() {
		defer h.wg.Done()
		if err := snmp.Serve(dev, auth, logs, h.stop); err != nil {
			log.Printf("[%s] SNMP agent error: %v", dev.Hostname, err)
		}
	}()
	return h
}

// stopHandle signals the device's servers to stop and waits for their
// listeners to close, so the IP:port can be reused immediately.
func (s *Simulator) stopHandle(h *deviceHandle) {
	close(h.stop)
	h.wg.Wait()
}
