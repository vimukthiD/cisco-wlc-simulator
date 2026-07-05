package device

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"text/template"
	"time"
)

// Device represents a simulated Cisco 9800-CL WLC.
type Device struct {
	Hostname  string    `yaml:"hostname" json:"hostname"`
	IP        string    `yaml:"ip" json:"ip"`
	HTTPSPort int       `yaml:"https_port" json:"https_port"`
	SSHPort   int       `yaml:"ssh_port" json:"ssh_port"`
	SNMPPort  int       `yaml:"snmp_port" json:"snmp_port"`
	TFTPPort  int       `yaml:"tftp_port" json:"tftp_port"`
	Model     string    `yaml:"model" json:"model"`
	Serial    string    `yaml:"serial" json:"serial"`
	Version   string    `yaml:"version" json:"version"`
	APs       []AP      `yaml:"aps" json:"aps"`
	StartTime time.Time `yaml:"-" json:"-"`

	// cachedConfig is the rendered config, built once at startup.
	cachedConfig string
}

// AP represents a simulated access point.
type AP struct {
	Name    string   `yaml:"name" json:"name"`
	MAC     string   `yaml:"mac" json:"mac"`
	Model   string   `yaml:"model" json:"model"`
	SSIDs   []string `yaml:"ssids" json:"ssids"`
	Clients []Client `yaml:"clients" json:"clients"`
	// Default marks the site's built-in AP. A site is itself an access point:
	// this AP is created automatically, named after the site hostname, and
	// cannot be removed or renamed (but is otherwise editable).
	Default bool `yaml:"default,omitempty" json:"default,omitempty"`
}

// defaultAPModel is the model assigned to a site's auto-created default AP.
const defaultAPModel = "C9120AXI-B"

// EnsureDefaultAP guarantees the site has exactly one locked "default" AP whose
// name mirrors the site hostname. It is idempotent, so it is safe to call on
// every path that creates or loads a site (seed load, runtime add, import).
func (d *Device) EnsureDefaultAP() {
	if d.Hostname == "" {
		return
	}
	// Already marked: keep it, just re-sync the derived fields.
	for i := range d.APs {
		if d.APs[i].Default {
			fillDefaultAP(&d.APs[i], d.Hostname)
			return
		}
	}
	// Unmarked config (e.g. a pre-feature export) that already has an AP named
	// after the site: adopt it as the default rather than duplicating.
	for i := range d.APs {
		if d.APs[i].Name == d.Hostname {
			d.APs[i].Default = true
			fillDefaultAP(&d.APs[i], d.Hostname)
			return
		}
	}
	// Otherwise create it, listed first as the site's primary AP.
	ap := AP{Default: true}
	fillDefaultAP(&ap, d.Hostname)
	d.APs = append([]AP{ap}, d.APs...)
}

// fillDefaultAP sets the name and any missing derived fields on a default AP.
func fillDefaultAP(ap *AP, hostname string) {
	ap.Name = hostname
	if ap.MAC == "" {
		ap.MAC = defaultAPMAC(hostname)
	}
	if ap.Model == "" {
		ap.Model = defaultAPModel
	}
}

// defaultAPMAC derives a stable MAC for a site's default AP from its hostname.
func defaultAPMAC(hostname string) string {
	h := fnv.New32a()
	h.Write([]byte(hostname))
	sum := h.Sum32()
	return fmt.Sprintf("00:3a:7d:%02x:%02x:%02x", byte(sum>>16), byte(sum>>8), byte(sum))
}

// Client represents a wireless client connected to an AP.
type Client struct {
	MAC            string   `yaml:"mac" json:"mac"`
	IPv4           string   `yaml:"ipv4" json:"ipv4"`
	IPv6           []string `yaml:"ipv6" json:"ipv6"`
	Username       string   `yaml:"username" json:"username"`
	Hostname       string   `yaml:"hostname" json:"hostname"`
	SSID           string   `yaml:"ssid" json:"ssid"`
	BSSID          string   `yaml:"bssid" json:"bssid"`
	WLANId         int      `yaml:"wlan_id" json:"wlan_id"`
	Channel        int      `yaml:"channel" json:"channel"`
	RadioType      string   `yaml:"radio_type" json:"radio_type"`
	SecurityMode   string   `yaml:"security_mode" json:"security_mode"`
	EncryptionType string   `yaml:"encryption_type" json:"encryption_type"`
	AuthKeyMgmt    string   `yaml:"auth_key_mgmt" json:"auth_key_mgmt"`
	RSSI           int      `yaml:"rssi" json:"rssi"`
	SNR            int      `yaml:"snr" json:"snr"`
	Speed          int      `yaml:"speed" json:"speed"`
	BytesRx        int64    `yaml:"bytes_rx" json:"bytes_rx"`
	BytesTx        int64    `yaml:"bytes_tx" json:"bytes_tx"`
	PktsRx         int64    `yaml:"pkts_rx" json:"pkts_rx"`
	PktsTx         int64    `yaml:"pkts_tx" json:"pkts_tx"`
	DataRetries    int64    `yaml:"data_retries" json:"data_retries"`
	DeviceType     string   `yaml:"device_type" json:"device_type"`
	DeviceOS       string   `yaml:"device_os" json:"device_os"`
	VLAN           int      `yaml:"vlan" json:"vlan"`
	VLANName       string   `yaml:"vlan_name" json:"vlan_name"`
	State          string   `yaml:"state" json:"state"`
	PolicyProfile  string   `yaml:"policy_profile" json:"policy_profile"`
	WLANProfile    string   `yaml:"wlan_profile" json:"wlan_profile"`
	AssocTime      string   `yaml:"assoc_time" json:"assoc_time"`
	SpatialStreams int      `yaml:"spatial_streams" json:"spatial_streams"`
}

// AllClients returns all clients across all APs for this device.
func (d *Device) AllClients() []ClientWithAP {
	var result []ClientWithAP
	for _, ap := range d.APs {
		for _, c := range ap.Clients {
			result = append(result, ClientWithAP{Client: c, AP: ap})
		}
	}
	return result
}

// ClientWithAP pairs a client with its parent AP.
type ClientWithAP struct {
	Client Client
	AP     AP
}

// StartupConfig returns the startup configuration (saved config).
func (d *Device) StartupConfig() string {
	return d.RunningConfig()
}

// RunningConfig returns the rendered Cisco IOS-XE config for this device.
// Lazily initializes if the config hasn't been rendered yet.
func (d *Device) RunningConfig() string {
	if d.cachedConfig == "" {
		d.InitConfig("")
	}
	return d.cachedConfig
}

// templateData is the context passed to the config template.
type templateData struct {
	Hostname   string
	IP         string
	Version    string
	Serial     string
	SerialHash string
	VLANs      []vlanEntry
	WLANs      []wlanEntry
	APs        []AP
}

type vlanEntry struct {
	ID   int
	Name string
}

type wlanEntry struct {
	SSID string
	ID   int
	AKM  string
}

// InitConfig renders the config template with device-specific values.
// Call this once after loading the config. If tmplText is empty, the
// built-in default template is used.
func (d *Device) InitConfig(tmplText string) {
	if tmplText == "" {
		tmplText = defaultConfigTemplate
	}

	tmpl, err := template.New("config").Parse(tmplText)
	if err != nil {
		d.cachedConfig = fmt.Sprintf("! template error: %v\n", err)
		return
	}

	// Collect VLANs and WLANs from client data
	vlans := map[int]string{}
	ssids := map[string]string{} // ssid -> akm
	for _, ap := range d.APs {
		for _, c := range ap.Clients {
			if c.VLAN > 0 {
				name := c.VLANName
				if name == "" {
					name = fmt.Sprintf("VLAN%04d", c.VLAN)
				}
				vlans[c.VLAN] = name
			}
			if c.SSID != "" {
				akm := c.AuthKeyMgmt
				if akm == "" {
					akm = "dot1x"
				}
				ssids[c.SSID] = akm
			}
		}
	}

	var vlanList []vlanEntry
	for id, name := range vlans {
		vlanList = append(vlanList, vlanEntry{ID: id, Name: name})
	}
	var wlanList []wlanEntry
	wlanID := 1
	for ssid, akm := range ssids {
		wlanList = append(wlanList, wlanEntry{SSID: ssid, ID: wlanID, AKM: akm})
		wlanID++
	}

	h := fnv.New32a()
	h.Write([]byte(d.Serial))
	serialHash := fmt.Sprintf("%d", h.Sum32())

	data := templateData{
		Hostname:   d.Hostname,
		IP:         d.IP,
		Version:    d.Version,
		Serial:     d.Serial,
		SerialHash: serialHash,
		VLANs:      vlanList,
		WLANs:      wlanList,
		APs:        d.APs,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		d.cachedConfig = fmt.Sprintf("! template exec error: %v\n", err)
		return
	}
	d.cachedConfig = buf.String()
}

const defaultConfigTemplate = `!
! Last configuration change at 12:30:45 UTC Mon Mar 15 2024
! NVRAM config last updated at 12:30:50 UTC Mon Mar 15 2024
!
version {{.Version}}
service timestamps debug datetime msec localtime show-timezone
service timestamps log datetime msec localtime show-timezone
service password-encryption
platform qfp utilization monitor load 80
!
hostname {{.Hostname}}
!
boot-start-marker
boot system flash bootflash:packages.conf
boot-end-marker
!
logging buffered 16384 informational
enable secret 9 $9$nGSHsEuW4k$z5kFVGO/S0LJxmUoRKFnEfLflk0v3Q4aCXwklRKLqjA
!
aaa new-model
aaa authentication login default local
aaa authentication login CONSOLE local
aaa authorization console
aaa authorization exec default local
!
ip domain name wlc.local
ip name-server 8.8.8.8
!
username admin privilege 15 secret 9 $9$nGSHsEuW4k$z5kFVGO/S0LJxmUoRKFnEfLflk0v3Q4aCXwklRKLqjA
!
redundancy
 mode sso
!
{{range .VLANs}}vlan {{.ID}}
 name {{.Name}}
!
{{end}}interface GigabitEthernet1
 ip address {{.IP}} 255.255.255.0
 negotiation auto
 no shutdown
!
interface Loopback0
 ip address {{.IP}} 255.255.255.255
!
{{range .WLANs}}wlan {{.SSID}} {{.ID}} {{.SSID}}
 security wpa wpa2
 security wpa akm {{.AKM}}
 no shutdown
!
{{end}}wireless aaa policy default-aaa-policy
wireless profile policy default-policy-profile
 vlan default
 no shutdown
!
wireless country US
!
restconf
!
ip http server
ip http secure-server
ip http authentication local
!
ip ssh version 2
ip scp server enable
!
snmp-server community public RO
snmp-server location {{.Hostname}}
snmp-server contact admin@{{.Hostname}}.local
snmp-server chassis-id {{.Serial}}
!
line con 0
 stopbits 1
line vty 0 15
 privilege level 15
 transport input ssh
!
end
`
