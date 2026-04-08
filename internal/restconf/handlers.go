package restconf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/vimukthi/cisco-wlc-sim/internal/device"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/yang-data+json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func handleClientOperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:client-oper-data": map[string]any{
			"common-oper-data": buildCommonOperData(clients),
			"dot11-oper-data":  buildDot11OperData(clients),
			"traffic-stats":    buildTrafficStats(clients),
			"sisf-db-mac":      buildSisfDbMac(clients),
			"dc-info":          buildDcInfo(clients),
			"policy-data":      buildPolicyData(clients),
		},
	}
	writeJSON(w, resp)
}

func handleCommonOperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:common-oper-data": buildCommonOperData(clients),
	}
	writeJSON(w, resp)
}

func handleDot11OperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:dot11-oper-data": buildDot11OperData(clients),
	}
	writeJSON(w, resp)
}

func handleTrafficStats(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:traffic-stats": buildTrafficStats(clients),
	}
	writeJSON(w, resp)
}

func handleSisfDbMac(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:sisf-db-mac": buildSisfDbMac(clients),
	}
	writeJSON(w, resp)
}

func handleDcInfo(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:dc-info": buildDcInfo(clients),
	}
	writeJSON(w, resp)
}

func handlePolicyData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:policy-data": buildPolicyData(clients),
	}
	writeJSON(w, resp)
}

// --- builders ---

func assocTime(c device.Client) string {
	if c.AssocTime != "" {
		return c.AssocTime
	}
	return time.Now().Add(-30 * time.Minute).UTC().Format("2006-01-02T15:04:05.000+00:00")
}

func buildCommonOperData(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		entry := map[string]any{
			"client-mac":    c.MAC,
			"ap-name":       cwa.AP.Name,
			"ms-ap-slot-id": 1,
			"ms-radio-type": c.RadioType,
			"wlan-id":       c.WLANId,
			"client-type":   "dot11-client",
			"co-state":      c.State,
			"wlan-policy": map[string]any{
				"current-switching-mode":  "central",
				"wlan-switching-mode":     "central",
				"central-authentication":  true,
				"central-dhcp":            true,
				"central-assoc-enable":    true,
				"vlan-central-switching":  false,
				"is-fabric-client":        false,
				"is-guest-fabric-client":  false,
			},
			"username":                     c.Username,
			"method-id":                    methodID(c.AuthKeyMgmt),
			"idle-timeout":                 300,
			"session-timeout":              1800,
			"vrf-name":                     "",
			"is-locally-administered-mac":  false,
			"aaa-override-passphrase":      false,
		}
		result = append(result, entry)
	}
	return result
}

func buildDot11OperData(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		bssid := c.BSSID
		if bssid == "" {
			bssid = cwa.AP.MAC
		}
		entry := map[string]any{
			"ms-mac-address":   c.MAC,
			"dot11-state":      "dot11-state-associated",
			"ms-bssid":         bssid,
			"ap-mac-address":   cwa.AP.MAC,
			"current-channel":  c.Channel,
			"ms-wlan-id":       c.WLANId,
			"vap-ssid":         c.SSID,
			"policy-profile":   c.PolicyProfile,
			"ms-ap-slot-id":    1,
			"radio-type":       c.RadioType,
			"ms-assoc-time":    assocTime(c),
			"wlan-profile":     wlanProfile(c),
			"encryption-type":  c.EncryptionType,
			"security-mode":    c.SecurityMode,
			"dot11-6ghz-cap":   false,
			"ms-wifi": map[string]any{
				"wpa-version":              c.SecurityMode,
				"cipher-suite":             c.EncryptionType,
				"auth-key-mgmt":            c.AuthKeyMgmt,
				"group-mgmt-cipher-suite":  c.EncryptionType,
				"group-cipher-suite":       c.EncryptionType,
				"pwe-mode":                 "sae-pwe-mode-none",
			},
			"ms-wme-enabled":      true,
			"dot11w-enabled":      true,
			"bss-trans-capable":   true,
			"ewlc-ms-phy-type":   c.RadioType,
			"dms-capable":         false,
			"qosmap-capable":      true,
			"link-local-enable":   false,
		}
		result = append(result, entry)
	}
	return result
}

func buildTrafficStats(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		entry := map[string]any{
			"ms-mac-address":      c.MAC,
			"bytes-rx":            c.BytesRx,
			"bytes-tx":            c.BytesTx,
			"pkts-rx":             c.PktsRx,
			"pkts-tx":             c.PktsTx,
			"data-retries":        c.DataRetries,
			"rts-retries":         0,
			"duplicate-rcv":       0,
			"decrypt-failed":      0,
			"mic-mismatch":        0,
			"mic-missing":         0,
			"most-recent-rssi":    c.RSSI,
			"most-recent-snr":     c.SNR,
			"tx-excessive-retries": 0,
			"tx-retries":          c.DataRetries * 2,
			"power-save-state":    0,
			"current-rate":        fmt.Sprintf("%d.0", c.Speed),
			"speed":               c.Speed,
			"spatial-stream":      c.SpatialStreams,
			"client-active":       true,
			"rx-group-counter":    0,
			"tx-total-drops":      0,
		}
		result = append(result, entry)
	}
	return result
}

func buildSisfDbMac(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		entry := map[string]any{
			"mac-addr": c.MAC,
		}
		if c.IPv4 != "" {
			entry["ipv4-binding"] = map[string]any{
				"ip-key": map[string]any{
					"zone-id": 0,
					"ip-addr": c.IPv4,
				},
			}
		}
		if len(c.IPv6) > 0 {
			var v6 []map[string]any
			for _, addr := range c.IPv6 {
				v6 = append(v6, map[string]any{
					"ip-key": map[string]any{
						"zone-id": 0,
						"ip-addr": addr,
					},
				})
			}
			entry["ipv6-binding"] = v6
		}
		result = append(result, entry)
	}
	return result
}

func buildDcInfo(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		entry := map[string]any{
			"client-mac":       c.MAC,
			"device-type":      orDefault(c.DeviceType, "Microsoft-Workstation"),
			"confidence-level": 30,
			"device-os":        orDefault(c.DeviceOS, "Windows"),
			"device-name":      c.Hostname,
		}
		result = append(result, entry)
	}
	return result
}

func buildPolicyData(clients []device.ClientWithAP) []map[string]any {
	var result []map[string]any
	for _, cwa := range clients {
		c := cwa.Client
		entry := map[string]any{
			"mac":           c.MAC,
			"res-vlan-id":   c.VLAN,
			"res-vlan-name": c.VLANName,
		}
		result = append(result, entry)
	}
	return result
}

// --- helpers ---

func methodID(authKeyMgmt string) string {
	switch authKeyMgmt {
	case "8021x":
		return "dot1x-auth-id"
	case "psk":
		return "psk-auth-id"
	case "sae":
		return "sae-auth-id"
	default:
		return "dot1x-auth-id"
	}
}

func wlanProfile(c device.Client) string {
	if c.WLANProfile != "" {
		return c.WLANProfile
	}
	return c.SSID
}

func orDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}
