package snmp

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/gosnmp/gosnmp"
	GoSNMPServer "github.com/slayercat/GoSNMPServer"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/accesslog"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/config"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/device"
)

// Serve starts an SNMP agent for the given device.
func Serve(dev *device.Device, auth config.Auth, logs *accesslog.Store) error {
	oids := buildOIDs(dev, logs)

	master := GoSNMPServer.MasterAgent{
		Logger:         GoSNMPServer.NewDiscardLogger(),
		SecurityConfig: GoSNMPServer.SecurityConfig{},
		SubAgents: []*GoSNMPServer.SubAgent{
			{
				CommunityIDs: []string{auth.SNMPCommunity},
				OIDs:         oids,
				Logger:       GoSNMPServer.NewDiscardLogger(),
			},
		},
	}

	server := GoSNMPServer.NewSNMPServer(master)
	addr := fmt.Sprintf("%s:%d", dev.IP, dev.SNMPPort)
	if err := server.ListenUDP("udp", addr); err != nil {
		return fmt.Errorf("snmp listen %s: %w", addr, err)
	}

	log.Printf("[%s] SNMP listening on %s (community: %s)", dev.Hostname, addr, auth.SNMPCommunity)
	return server.ServeForever()
}

func buildOIDs(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	var oids []*GoSNMPServer.PDUValueControlItem
	oids = append(oids, sysGroup(dev, logs)...)
	oids = append(oids, snmpEngine(dev, logs)...)
	oids = append(oids, ifMIB(dev, logs)...)
	oids = append(oids, ipAddrTable(dev, logs)...)
	oids = append(oids, entityMIB(dev, logs)...)
	return oids
}

// loggedGet wraps an OnGet callback to record SNMP access.
func loggedGet(dev *device.Device, logs *accesslog.Store, oid string, fn func() (interface{}, error)) func() (interface{}, error) {
	return func() (interface{}, error) {
		val, err := fn()
		logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "snmp",
			Method:     "GET",
			Path:       oid,
		})
		return val, err
	}
}

// --- SNMPv2-MIB system group (.1.3.6.1.2.1.1) ---

func sysGroup(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	sysDescr := fmt.Sprintf(
		"Cisco IOS Software [Dublin], C9800-CL Software (%s), Version %s",
		dev.Model, dev.Version,
	)

	return []*GoSNMPServer.PDUValueControlItem{
		{
			OID:  "1.3.6.1.2.1.1.1.0",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "sysDescr", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(sysDescr), nil
			}),
			Document: "sysDescr",
		},
		{
			OID:  "1.3.6.1.2.1.1.2.0",
			Type: gosnmp.ObjectIdentifier,
			OnGet: loggedGet(dev, logs, "sysObjectID", func() (interface{}, error) {
				return GoSNMPServer.Asn1ObjectIdentifierWrap("1.3.6.1.4.1.9.1.2530"), nil
			}),
			Document: "sysObjectID",
		},
		{
			OID:  "1.3.6.1.2.1.1.3.0",
			Type: gosnmp.TimeTicks,
			OnGet: loggedGet(dev, logs, "sysUpTime", func() (interface{}, error) {
				uptime := uint32(time.Since(dev.StartTime).Seconds() * 100)
				return GoSNMPServer.Asn1TimeTicksWrap(uptime), nil
			}),
			Document: "sysUpTime",
		},
		{
			OID:  "1.3.6.1.2.1.1.4.0",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "sysContact", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(""), nil
			}),
			Document: "sysContact",
		},
		{
			OID:  "1.3.6.1.2.1.1.5.0",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "sysName", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(dev.Hostname), nil
			}),
			Document: "sysName",
		},
		{
			OID:  "1.3.6.1.2.1.1.6.0",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "sysLocation", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(""), nil
			}),
			Document: "sysLocation",
		},
		{
			OID:  "1.3.6.1.2.1.1.7.0",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "sysServices", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(72), nil
			}),
			Document: "sysServices",
		},
	}
}

// --- SNMP Engine (.1.3.6.1.6.3.10.2.1) ---

func snmpEngine(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	return []*GoSNMPServer.PDUValueControlItem{
		{
			OID:  "1.3.6.1.6.3.10.2.1.3.0",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "snmpEngineTime", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(int(time.Since(dev.StartTime).Seconds())), nil
			}),
			Document: "snmpEngineTime",
		},
	}
}

// --- IF-MIB (.1.3.6.1.2.1.2) ---

func ifMIB(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	mac := macFromIP(dev.IP)

	return []*GoSNMPServer.PDUValueControlItem{
		{
			OID:  "1.3.6.1.2.1.2.1.0",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifNumber", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1), nil
			}),
			Document: "ifNumber",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.1.1",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifIndex.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1), nil
			}),
			Document: "ifIndex",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.2.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "ifDescr.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap("GigabitEthernet1"), nil
			}),
			Document: "ifDescr",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.3.1",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifType.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(6), nil // ethernetCsmacd
			}),
			Document: "ifType",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.4.1",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifMtu.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1500), nil
			}),
			Document: "ifMtu",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.5.1",
			Type: gosnmp.Gauge32,
			OnGet: loggedGet(dev, logs, "ifSpeed.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1Gauge32Wrap(1000000000), nil // 1 Gbps
			}),
			Document: "ifSpeed",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.6.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "ifPhysAddress.1", func() (interface{}, error) {
				return string(mac), nil // raw 6-byte MAC
			}),
			Document: "ifPhysAddress",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.7.1",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifAdminStatus.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1), nil // up
			}),
			Document: "ifAdminStatus",
		},
		{
			OID:  "1.3.6.1.2.1.2.2.1.8.1",
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ifOperStatus.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1), nil // up
			}),
			Document: "ifOperStatus",
		},
		// IF-MIB extensions (.1.3.6.1.2.1.31.1.1.1)
		{
			OID:  "1.3.6.1.2.1.31.1.1.1.1.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "ifName.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap("Gi1"), nil
			}),
			Document: "ifName",
		},
		{
			OID:  "1.3.6.1.2.1.31.1.1.1.18.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "ifAlias.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap("Management Interface"), nil
			}),
			Document: "ifAlias",
		},
	}
}

// --- IP-MIB ipAddrTable (.1.3.6.1.2.1.4.20.1) ---

func ipAddrTable(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	// OID index for ipAddrTable is the IP address itself encoded as octets
	// e.g., 10.99.0.1 → .10.99.0.1
	ip := net.ParseIP(dev.IP).To4()
	if ip == nil {
		return nil
	}
	idx := fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])

	return []*GoSNMPServer.PDUValueControlItem{
		{
			OID:  "1.3.6.1.2.1.4.20.1.1." + idx,
			Type: gosnmp.IPAddress,
			OnGet: loggedGet(dev, logs, "ipAdEntAddr", func() (interface{}, error) {
				return GoSNMPServer.Asn1IPAddressWrap(ip), nil
			}),
			Document: "ipAdEntAddr",
		},
		{
			OID:  "1.3.6.1.2.1.4.20.1.2." + idx,
			Type: gosnmp.Integer,
			OnGet: loggedGet(dev, logs, "ipAdEntIfIndex", func() (interface{}, error) {
				return GoSNMPServer.Asn1IntegerWrap(1), nil // GigabitEthernet1
			}),
			Document: "ipAdEntIfIndex",
		},
		{
			OID:  "1.3.6.1.2.1.4.20.1.3." + idx,
			Type: gosnmp.IPAddress,
			OnGet: loggedGet(dev, logs, "ipAdEntNetMask", func() (interface{}, error) {
				return GoSNMPServer.Asn1IPAddressWrap(net.IPv4(255, 255, 255, 0).To4()), nil
			}),
			Document: "ipAdEntNetMask",
		},
	}
}

// --- ENTITY-MIB (.1.3.6.1.2.1.47.1.1.1.1) ---

func entityMIB(dev *device.Device, logs *accesslog.Store) []*GoSNMPServer.PDUValueControlItem {
	base := "1.3.6.1.2.1.47.1.1.1.1"

	return []*GoSNMPServer.PDUValueControlItem{
		{
			OID:  base + ".2.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "entPhysicalDescr.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap("Cisco Catalyst 9800-CL Wireless Controller"), nil
			}),
			Document: "entPhysicalDescr",
		},
		{
			OID:  base + ".10.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "entPhysicalSoftwareRev.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(dev.Version), nil
			}),
			Document: "entPhysicalSoftwareRev",
		},
		{
			OID:  base + ".11.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "entPhysicalSerialNum.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(dev.Serial), nil
			}),
			Document: "entPhysicalSerialNum",
		},
		{
			OID:  base + ".12.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "entPhysicalMfgName.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap("Cisco Systems, Inc."), nil
			}),
			Document: "entPhysicalMfgName",
		},
		{
			OID:  base + ".13.1",
			Type: gosnmp.OctetString,
			OnGet: loggedGet(dev, logs, "entPhysicalModelName.1", func() (interface{}, error) {
				return GoSNMPServer.Asn1OctetStringWrap(dev.Model), nil
			}),
			Document: "entPhysicalModelName",
		},
	}
}

// --- helpers ---

// macFromIP derives a deterministic locally-administered MAC from the device IP.
func macFromIP(ip string) net.HardwareAddr {
	parts := net.ParseIP(ip).To4()
	if parts == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}
	return net.HardwareAddr{0x02, 0xc9, parts[0], parts[1], parts[2], parts[3]}
}
