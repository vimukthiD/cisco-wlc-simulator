package device

// Device represents a simulated Cisco 9800-CL WLC.
type Device struct {
	Hostname  string `yaml:"hostname" json:"hostname"`
	IP        string `yaml:"ip" json:"ip"`
	HTTPSPort int    `yaml:"https_port" json:"https_port"`
	SSHPort   int    `yaml:"ssh_port" json:"ssh_port"`
	Model     string `yaml:"model" json:"model"`
	Serial    string `yaml:"serial" json:"serial"`
	Version   string `yaml:"version" json:"version"`
	APs       []AP   `yaml:"aps" json:"aps"`
}

// AP represents a simulated access point.
type AP struct {
	Name    string   `yaml:"name" json:"name"`
	MAC     string   `yaml:"mac" json:"mac"`
	Model   string   `yaml:"model" json:"model"`
	Clients []Client `yaml:"clients" json:"clients"`
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
