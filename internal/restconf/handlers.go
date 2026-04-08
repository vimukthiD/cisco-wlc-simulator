package restconf

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/device"
)

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "json")
}

func writeResponse(w http.ResponseWriter, r *http.Request, v any, xmlRoot string, xmlNS string) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/yang-data+json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(v)
		return
	}
	// Default: XML
	w.Header().Set("Content-Type", "application/yang-data+xml")
	xmlData := toXML(v, xmlRoot, xmlNS)
	w.Write([]byte(xml.Header))
	w.Write(xmlData)
}

// toXML renders a map[string]any structure as YANG-style XML.
func toXML(v any, rootName string, ns string) []byte {
	var sb strings.Builder

	// Unwrap the top-level map: {"Cisco-...:name": <inner>}
	var inner any
	if m, ok := v.(map[string]any); ok {
		for _, val := range m {
			inner = val
			break
		}
	}

	// If inner is a list ([]map[string]any), wrap each item in the root element
	if items, ok := inner.([]map[string]any); ok {
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("<%s xmlns=\"%s\">\n", rootName, ns))
			writeXMLMap(&sb, item, 1)
			sb.WriteString(fmt.Sprintf("</%s>\n", rootName))
		}
		return []byte(sb.String())
	}

	// Otherwise it's a map (the full client-oper-data container)
	sb.WriteString(fmt.Sprintf("<%s xmlns=\"%s\">\n", rootName, ns))
	if m, ok := inner.(map[string]any); ok {
		writeXMLMap(&sb, m, 1)
	}
	sb.WriteString(fmt.Sprintf("</%s>\n", rootName))
	return []byte(sb.String())
}

func writeXMLMap(sb *strings.Builder, m map[string]any, indent int) {
	prefix := strings.Repeat("  ", indent)
	for key, val := range m {
		switch v := val.(type) {
		case []map[string]any:
			for _, item := range v {
				sb.WriteString(fmt.Sprintf("%s<%s>\n", prefix, key))
				writeXMLMap(sb, item, indent+1)
				sb.WriteString(fmt.Sprintf("%s</%s>\n", prefix, key))
			}
		case map[string]any:
			sb.WriteString(fmt.Sprintf("%s<%s>\n", prefix, key))
			writeXMLMap(sb, v, indent+1)
			sb.WriteString(fmt.Sprintf("%s</%s>\n", prefix, key))
		case []any:
			for _, item := range v {
				if im, ok := item.(map[string]any); ok {
					sb.WriteString(fmt.Sprintf("%s<%s>\n", prefix, key))
					writeXMLMap(sb, im, indent+1)
					sb.WriteString(fmt.Sprintf("%s</%s>\n", prefix, key))
				} else {
					sb.WriteString(fmt.Sprintf("%s<%s>%v</%s>\n", prefix, key, item, key))
				}
			}
		case bool:
			if v {
				sb.WriteString(fmt.Sprintf("%s<%s>true</%s>\n", prefix, key, key))
			} else {
				sb.WriteString(fmt.Sprintf("%s<%s>false</%s>\n", prefix, key, key))
			}
		default:
			sb.WriteString(fmt.Sprintf("%s<%s>%v</%s>\n", prefix, key, val, key))
		}
	}
}

func writeXMLValue(sb *strings.Builder, val any, indent int) {
	switch v := val.(type) {
	case map[string]any:
		writeXMLMap(sb, v, indent)
	default:
		sb.WriteString(fmt.Sprintf("%v", val))
	}
}

const yangNS = "http://cisco.com/ns/yang/Cisco-IOS-XE-wireless-client-oper"

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
	writeResponse(w, r, resp, "client-oper-data", yangNS)
}

func handleCommonOperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:common-oper-data": buildCommonOperData(clients),
	}
	writeResponse(w, r, resp, "common-oper-data", yangNS)
}

func handleDot11OperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:dot11-oper-data": buildDot11OperData(clients),
	}
	writeResponse(w, r, resp, "dot11-oper-data", yangNS)
}

func handleTrafficStats(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:traffic-stats": buildTrafficStats(clients),
	}
	writeResponse(w, r, resp, "traffic-stats", yangNS)
}

func handleSisfDbMac(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:sisf-db-mac": buildSisfDbMac(clients),
	}
	writeResponse(w, r, resp, "sisf-db-mac", yangNS)
}

func handleDcInfo(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:dc-info": buildDcInfo(clients),
	}
	writeResponse(w, r, resp, "dc-info", yangNS)
}

func handlePolicyData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	clients := dev.AllClients()
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-client-oper:policy-data": buildPolicyData(clients),
	}
	writeResponse(w, r, resp, "policy-data", yangNS)
}

const apYangNS = "http://cisco.com/ns/yang/Cisco-IOS-XE-wireless-access-point-oper"

func handleAPOperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-access-point-oper:access-point-oper-data": map[string]any{
			"capwap-data":     buildCapwapData(dev),
			"ap-name-mac-map": buildAPNameMacMap(dev),
			"radio-oper-data": buildRadioOperData(dev),
		},
	}
	writeResponse(w, r, resp, "access-point-oper-data", apYangNS)
}

func handleCapwapData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-access-point-oper:capwap-data": buildCapwapData(dev),
	}
	writeResponse(w, r, resp, "capwap-data", apYangNS)
}

func handleAPNameMacMap(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-access-point-oper:ap-name-mac-map": buildAPNameMacMap(dev),
	}
	writeResponse(w, r, resp, "ap-name-mac-map", apYangNS)
}

func handleRadioOperData(w http.ResponseWriter, r *http.Request, dev *device.Device) {
	resp := map[string]any{
		"Cisco-IOS-XE-wireless-access-point-oper:radio-oper-data": buildRadioOperData(dev),
	}
	writeResponse(w, r, resp, "radio-oper-data", apYangNS)
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
				"current-switching-mode": "central",
				"wlan-switching-mode":    "central",
				"central-authentication": true,
				"central-dhcp":           true,
				"central-assoc-enable":   true,
				"vlan-central-switching": false,
				"is-fabric-client":       false,
				"is-guest-fabric-client": false,
			},
			"username":                    c.Username,
			"method-id":                   methodID(c.AuthKeyMgmt),
			"idle-timeout":                300,
			"session-timeout":             1800,
			"vrf-name":                    "",
			"is-locally-administered-mac": false,
			"aaa-override-passphrase":     false,
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
			"ms-mac-address":  c.MAC,
			"dot11-state":     "dot11-state-associated",
			"ms-bssid":        bssid,
			"ap-mac-address":  cwa.AP.MAC,
			"current-channel": c.Channel,
			"ms-wlan-id":      c.WLANId,
			"vap-ssid":        c.SSID,
			"policy-profile":  c.PolicyProfile,
			"ms-ap-slot-id":   1,
			"radio-type":      c.RadioType,
			"ms-assoc-time":   assocTime(c),
			"wlan-profile":    wlanProfile(c),
			"encryption-type": c.EncryptionType,
			"security-mode":   c.SecurityMode,
			"dot11-6ghz-cap":  false,
			"ms-wifi": map[string]any{
				"wpa-version":             c.SecurityMode,
				"cipher-suite":            c.EncryptionType,
				"auth-key-mgmt":           c.AuthKeyMgmt,
				"group-mgmt-cipher-suite": c.EncryptionType,
				"group-cipher-suite":      c.EncryptionType,
				"pwe-mode":                "sae-pwe-mode-none",
			},
			"ms-wme-enabled":    true,
			"dot11w-enabled":    true,
			"bss-trans-capable": true,
			"ewlc-ms-phy-type":  c.RadioType,
			"dms-capable":       false,
			"qosmap-capable":    true,
			"link-local-enable": false,
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
			"ms-mac-address":       c.MAC,
			"bytes-rx":             c.BytesRx,
			"bytes-tx":             c.BytesTx,
			"pkts-rx":              c.PktsRx,
			"pkts-tx":              c.PktsTx,
			"data-retries":         c.DataRetries,
			"rts-retries":          0,
			"duplicate-rcv":        0,
			"decrypt-failed":       0,
			"mic-mismatch":         0,
			"mic-missing":          0,
			"most-recent-rssi":     c.RSSI,
			"most-recent-snr":      c.SNR,
			"tx-excessive-retries": 0,
			"tx-retries":           c.DataRetries * 2,
			"power-save-state":     0,
			"current-rate":         fmt.Sprintf("%d.0", c.Speed),
			"speed":                c.Speed,
			"spatial-stream":       c.SpatialStreams,
			"client-active":        true,
			"rx-group-counter":     0,
			"tx-total-drops":       0,
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

// --- AP oper data builders ---

func buildCapwapData(dev *device.Device) []map[string]any {
	var result []map[string]any
	for _, ap := range dev.APs {
		clientCount := len(ap.Clients)
		model := ap.Model
		if model == "" {
			model = "C9120AXI-B"
		}
		entry := map[string]any{
			"wtp-mac": ap.MAC,
			"ap-name": ap.Name,
			"device-detail": map[string]any{
				"static-info": map[string]any{
					"board-data": map[string]any{
						"wtp-model-no":  model,
						"wtp-serial-no": "",
					},
					"ap-model-name": model,
				},
				"ap-ip-addr":  dev.IP,
				"wtp-version": dev.Version,
			},
			"tag-info": map[string]any{
				"site-tag": map[string]any{
					"site-tag-name": "default-site-tag",
				},
				"policy-tag": map[string]any{
					"policy-tag-name": "default-policy-tag",
				},
				"rf-tag": map[string]any{
					"rf-tag-name": "default-rf-tag",
				},
			},
			"admin-state":        true,
			"client-count":       clientCount,
			"ap-operation-state": "ap-state-registered",
			"ap-join-state":      "ap-join-state-joined",
		}
		result = append(result, entry)
	}
	return result
}

func buildAPNameMacMap(dev *device.Device) []map[string]any {
	var result []map[string]any
	for _, ap := range dev.APs {
		result = append(result, map[string]any{
			"wtp-name": ap.Name,
			"wtp-mac":  ap.MAC,
		})
	}
	return result
}

func buildRadioOperData(dev *device.Device) []map[string]any {
	var result []map[string]any
	for _, ap := range dev.APs {
		// Slot 0: 2.4 GHz
		result = append(result, map[string]any{
			"wtp-mac":          ap.MAC,
			"radio-slot-id":    0,
			"slot-id":          0,
			"radio-type":       "radio-type-dot11bg",
			"oper-state":       true,
			"radio-mode":       "radio-mode-local",
			"current-channel":  6,
			"current-tx-power": 17,
			"channel-width":    "cw-20-mhz",
			"station-count":    0,
			"utilization":      10,
			"noise":            -95,
		})
		// Slot 1: 5 GHz
		clientCount := 0
		ch := 36
		for _, c := range ap.Clients {
			if c.Channel >= 36 {
				clientCount++
				ch = c.Channel
			}
		}
		result = append(result, map[string]any{
			"wtp-mac":          ap.MAC,
			"radio-slot-id":    1,
			"slot-id":          1,
			"radio-type":       "radio-type-dot11a",
			"oper-state":       true,
			"radio-mode":       "radio-mode-local",
			"current-channel":  ch,
			"current-tx-power": 20,
			"channel-width":    "cw-80-mhz",
			"station-count":    clientCount,
			"utilization":      25,
			"noise":            -92,
		})
	}
	return result
}
