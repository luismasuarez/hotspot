package hotspot

import (
	"fmt"
	"strings"
)

const (
	defaultSSID     = "WIFI_GRATIS"
	defaultSubnet   = "10.42.42.0/24"
	defaultAPName   = "hspot0"
	defaultTable    = 4242
	defaultPriority = 9000
	defaultDNS      = "8.8.8.8"
	defaultMSS      = 1360
	nftTableName    = "inet hotspot"

	runDir      = "/run/hotspot"
	statePath   = runDir + "/state.json"
	hostapdConf = runDir + "/hostapd.conf"
	hostapdPID  = runDir + "/hostapd.pid"
	dnsmasqConf = runDir + "/dnsmasq.conf"
	dnsmasqPID  = runDir + "/dnsmasq.pid"
)

// Source identifies where the hotspot gets its internet from.
type Source string

const (
	SourceEth  Source = "eth"
	SourceWiFi Source = "wifi"
	SourceVPN  Source = "vpn"
)

// ParseSource validates a source string.
func ParseSource(s string) (Source, error) {
	switch Source(strings.ToLower(strings.TrimSpace(s))) {
	case SourceEth:
		return SourceEth, nil
	case SourceWiFi:
		return SourceWiFi, nil
	case SourceVPN:
		return SourceVPN, nil
	}
	return "", fmt.Errorf("origen inválido %q (usa eth, wifi o vpn)", s)
}

// Config is the fully resolved configuration for one hotspot session.
type Config struct {
	Source    Source
	Uplink    string // resolved source interface providing internet
	WiFiIface string // wireless interface used to build the AP
	SSID      string
	Pass      string
	Open      bool
	Hidden    bool
	Band      string // "2.4" or "5"
	Channel   int
	Subnet    string
	DNS       string
	APName    string
	Table     int
	Priority  int
	MSS       int
	MTU       int // MTU advertised to clients (0 = don't advertise)
	DryRun    bool
	Dedicated bool   // true when APName is a virtual iface created by us
	NoQR      bool   // skip printing the QR after 'up'
	QRInvert  bool   // invert QR colors for dark terminals
	QRPNG     string // optional path to also save a PNG
}
