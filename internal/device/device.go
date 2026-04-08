package device

// Device represents a simulated Cisco 9800-CL WLC.
type Device struct {
	Hostname  string `yaml:"hostname"`
	IP        string `yaml:"ip"`
	HTTPSPort int    `yaml:"https_port"`
	SSHPort   int    `yaml:"ssh_port"`
	Model     string `yaml:"model"`
	Serial    string `yaml:"serial"`
	Version   string `yaml:"version"`
	APs       []AP   `yaml:"aps"`
}

// AP represents a simulated access point.
type AP struct {
	Name     string   `yaml:"name"`
	MAC      string   `yaml:"mac"`
	Model    string   `yaml:"model"`
	Clients  []Client `yaml:"clients"`
}

// Client represents a wireless client connected to an AP.
type Client struct {
	MAC            string   `yaml:"mac"`
	IPv4           string   `yaml:"ipv4"`
	IPv6           []string `yaml:"ipv6"`
	Username       string   `yaml:"username"`
	Hostname       string   `yaml:"hostname"`
	SSID           string   `yaml:"ssid"`
	BSSID          string   `yaml:"bssid"`
	WLANId         int      `yaml:"wlan_id"`
	Channel        int      `yaml:"channel"`
	RadioType      string   `yaml:"radio_type"`
	SecurityMode   string   `yaml:"security_mode"`
	EncryptionType string   `yaml:"encryption_type"`
	AuthKeyMgmt    string   `yaml:"auth_key_mgmt"`
	RSSI           int      `yaml:"rssi"`
	SNR            int      `yaml:"snr"`
	Speed          int      `yaml:"speed"`
	BytesRx        int64    `yaml:"bytes_rx"`
	BytesTx        int64    `yaml:"bytes_tx"`
	PktsRx         int64    `yaml:"pkts_rx"`
	PktsTx         int64    `yaml:"pkts_tx"`
	DataRetries    int64    `yaml:"data_retries"`
	DeviceType     string   `yaml:"device_type"`
	DeviceOS       string   `yaml:"device_os"`
	VLAN           int      `yaml:"vlan"`
	VLANName       string   `yaml:"vlan_name"`
	State          string   `yaml:"state"`
	PolicyProfile  string   `yaml:"policy_profile"`
	WLANProfile    string   `yaml:"wlan_profile"`
	AssocTime      string   `yaml:"assoc_time"`
	SpatialStreams int      `yaml:"spatial_streams"`
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
