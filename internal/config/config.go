package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vimukthi/cisco-wlc-sim/internal/device"
	"gopkg.in/yaml.v3"
)

// Config is the top-level simulator configuration.
type Config struct {
	Auth     Auth            `yaml:"auth"`
	Devices  []device.Device `yaml:"devices"`
	TmplText string          `yaml:"-"` // loaded config template text
}

// Auth holds credentials for RESTCONF, SSH, and SNMP.
type Auth struct {
	Username      string `yaml:"username" json:"username"`
	Password      string `yaml:"password" json:"password"`
	SNMPCommunity string `yaml:"snmp_community" json:"snmp_community"`
}

// Load reads and parses the YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		if d.HTTPSPort == 0 {
			d.HTTPSPort = 443
		}
		if d.SSHPort == 0 {
			d.SSHPort = 22
		}
		if d.SNMPPort == 0 {
			d.SNMPPort = 161
		}
		// TFTPPort defaults to 0 (disabled). Set to 69 in config to enable.
		if d.Model == "" {
			d.Model = "C9800-CL-K9"
		}
		if d.Version == "" {
			d.Version = "17.12.1"
		}
		// Default client fields
		for j := range d.APs {
			for k := range d.APs[j].Clients {
				c := &d.APs[j].Clients[k]
				if c.RadioType == "" {
					c.RadioType = "client-radio-type-11ax-5ghz"
				}
				if c.SecurityMode == "" {
					c.SecurityMode = "ewlc-assoc-mode-wpa2"
				}
				if c.EncryptionType == "" {
					c.EncryptionType = "encryp-policy-aes-ccm128"
				}
				if c.AuthKeyMgmt == "" {
					c.AuthKeyMgmt = "8021x"
				}
				if c.State == "" {
					c.State = "co-client-run"
				}
				if c.Channel == 0 {
					c.Channel = 36
				}
				if c.RSSI == 0 {
					c.RSSI = -55
				}
				if c.SNR == 0 {
					c.SNR = 35
				}
				if c.Speed == 0 {
					c.Speed = 573
				}
				if c.SpatialStreams == 0 {
					c.SpatialStreams = 2
				}
				if c.VLAN == 0 {
					c.VLAN = 50
				}
				if c.VLANName == "" {
					c.VLANName = fmt.Sprintf("VLAN%04d", c.VLAN)
				}
				if c.WLANId == 0 {
					c.WLANId = 1
				}
				if c.PolicyProfile == "" {
					c.PolicyProfile = "default-policy-profile"
				}
			}
			// Auto-populate AP SSIDs from client SSIDs if not explicitly set
			ap := &d.APs[j]
			if len(ap.SSIDs) == 0 {
				seen := map[string]bool{}
				for _, c := range ap.Clients {
					if c.SSID != "" && !seen[c.SSID] {
						ap.SSIDs = append(ap.SSIDs, c.SSID)
						seen[c.SSID] = true
					}
				}
			}
		}
	}

	if cfg.Auth.Username == "" {
		cfg.Auth.Username = "admin"
	}
	if cfg.Auth.Password == "" {
		cfg.Auth.Password = "admin"
	}
	if cfg.Auth.SNMPCommunity == "" {
		cfg.Auth.SNMPCommunity = "public"
	}

	// Load config template if it exists alongside the config file
	tmplPath := filepath.Join(filepath.Dir(path), "running-config.tmpl")
	if data, err := os.ReadFile(tmplPath); err == nil {
		cfg.TmplText = string(data)
	}

	// Render config for each device
	for i := range cfg.Devices {
		cfg.Devices[i].InitConfig(cfg.TmplText)
	}

	return &cfg, nil
}
