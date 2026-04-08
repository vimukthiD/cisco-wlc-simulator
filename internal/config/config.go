package config

import (
	"fmt"
	"os"

	"github.com/vimukthi/cisco-wlc-sim/internal/device"
	"gopkg.in/yaml.v3"
)

// Config is the top-level simulator configuration.
type Config struct {
	Auth    Auth            `yaml:"auth"`
	Devices []device.Device `yaml:"devices"`
}

// Auth holds credentials for RESTCONF and SSH.
type Auth struct {
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
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
		}
	}

	if cfg.Auth.Username == "" {
		cfg.Auth.Username = "admin"
	}
	if cfg.Auth.Password == "" {
		cfg.Auth.Password = "admin"
	}

	return &cfg, nil
}
