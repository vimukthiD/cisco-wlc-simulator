package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/device"
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

// stateHeader is prepended to persisted/exported YAML so the file is
// self-documenting when opened by hand.
const stateHeader = `# Cisco 9800-CL WLC Simulator configuration.
# When saved as the runtime state file this is auto-generated on every change;
# delete it (or use Factory Reset in the dashboard) to revert to the seed devices.yaml.
`

// Load reads and parses a YAML config file, applying defaults and rendering
// each device's running-config from the template found alongside it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}

	// Load config template if it exists alongside the config file
	tmplPath := filepath.Join(filepath.Dir(path), "running-config.tmpl")
	if tmplData, err := os.ReadFile(tmplPath); err == nil {
		cfg.TmplText = string(tmplData)
	}

	// Render config for each device
	for i := range cfg.Devices {
		cfg.Devices[i].InitConfig(cfg.TmplText)
	}

	return cfg, nil
}

// ParseYAML parses raw YAML bytes into a Config and applies defaults. It does
// not load a template or render device configs — callers that need a rendered
// config should call InitConfig (Load does this; the simulator does it when
// importing). Used for both Load and dashboard import.
func ParseYAML(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// Marshal serializes a Config back to the devices.yaml format (auth + devices),
// prefixed with a self-documenting header. Transient fields (StartTime, cached
// config) are excluded via their yaml:"-" tags.
func Marshal(cfg *Config) ([]byte, error) {
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return append([]byte(stateHeader), body...), nil
}

// Save atomically writes a Config to path (temp file + rename) so a crash
// mid-write can never leave a truncated state file.
func Save(path string, cfg *Config) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

// applyDefaults fills in defaults for devices, clients, auth, and AP SSIDs.
// It is idempotent, so persisted configs (which already contain expanded
// defaults) round-trip cleanly.
func applyDefaults(cfg *Config) {
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
}
