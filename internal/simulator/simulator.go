package simulator

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/config"
	"github.com/vimukthi/cisco-wlc-sim/internal/device"
	"github.com/vimukthi/cisco-wlc-sim/internal/network"
	"github.com/vimukthi/cisco-wlc-sim/internal/restconf"
	"github.com/vimukthi/cisco-wlc-sim/internal/snmp"
	"github.com/vimukthi/cisco-wlc-sim/internal/sshsim"
	"github.com/vimukthi/cisco-wlc-sim/internal/tftpsim"
)

// Simulator manages the lifecycle of simulated WLC devices.
// It is safe for concurrent use.
type Simulator struct {
	mu       sync.RWMutex
	devices  []*device.Device
	auth     config.Auth
	logs     *accesslog.Store
	tftpMgr  *tftpsim.Manager
	tmplText string
}

// New creates a Simulator from the loaded config.
func New(cfg *config.Config, logs *accesslog.Store, tmplText string) *Simulator {
	sim := &Simulator{
		auth:     cfg.Auth,
		logs:     logs,
		tftpMgr:  tftpsim.NewManager(logs),
		tmplText: tmplText,
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

// DevicePointers returns pointers to all devices (for config serialization).
func (s *Simulator) DevicePointers() []*device.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*device.Device, len(s.devices))
	copy(result, s.devices)
	return result
}

// Auth returns the shared auth config.
func (s *Simulator) Auth() config.Auth {
	return s.auth
}

// StartAll starts servers for all configured devices.
func (s *Simulator) StartAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, dev := range s.devices {
		s.startDeviceServers(dev)
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

	dev.StartTime = time.Now()
	dev.InitConfig(s.tmplText)

	// Add IP alias
	if err := network.AddIP(dev.IP); err != nil {
		return fmt.Errorf("add IP alias %s: %w", dev.IP, err)
	}

	s.mu.Lock()
	s.devices = append(s.devices, &dev)
	s.mu.Unlock()

	s.startDeviceServers(&dev)

	log.Printf("[%s] Device added at runtime: %s (HTTPS:%d, SSH:%d, SNMP:%d)",
		dev.Hostname, dev.IP, dev.HTTPSPort, dev.SSHPort, dev.SNMPPort)
	return nil
}

// RemoveDevice removes a device by IP, tears down its IP alias, and
// returns the removed device. The server goroutines will stop when
// their listeners are closed by the OS removing the IP.
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
	s.mu.Unlock()

	// Remove IP alias — listeners bound to this IP will fail/close
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
					d.APs = append(d.APs[:j], d.APs[j+1:]...)
					d.InitConfig(s.tmplText)
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
				return nil
			}
		}
		return fmt.Errorf("target AP %s not found on device %s", newAPName, deviceIP)
	}
	return fmt.Errorf("device with IP %s not found", deviceIP)
}

func (s *Simulator) startDeviceServers(dev *device.Device) {
	go func() {
		if err := restconf.Serve(dev, s.auth, s.logs); err != nil {
			log.Printf("[%s] RESTCONF server error: %v", dev.Hostname, err)
		}
	}()
	go func() {
		if err := sshsim.Serve(dev, s.auth, s.logs, s.tftpMgr); err != nil {
			log.Printf("[%s] SSH server error: %v", dev.Hostname, err)
		}
	}()
	go func() {
		if err := snmp.Serve(dev, s.auth, s.logs); err != nil {
			log.Printf("[%s] SNMP agent error: %v", dev.Hostname, err)
		}
	}()
}
