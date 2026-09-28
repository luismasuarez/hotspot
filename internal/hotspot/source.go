package hotspot

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type ipLink struct {
	Ifname    string   `json:"ifname"`
	Operstate string   `json:"operstate"`
	Flags     []string `json:"flags"`
	LinkType  string   `json:"link_type"`
	LinkInfo  *struct {
		InfoKind string `json:"info_kind"`
	} `json:"linkinfo"`
}

type ipRoute struct {
	Dst     string `json:"dst"`
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
}

// SourceInfo is a detected candidate uplink.
type SourceInfo struct {
	Kind   Source
	Iface  string
	Up     bool
	Detail string
}

// DetectSources lists the usable internet sources on the host.
func DetectSources() ([]SourceInfo, error) {
	raw, err := output("ip", "-j", "-d", "link", "show")
	if err != nil {
		return nil, fmt.Errorf("no se pudo listar interfaces: %w", err)
	}
	var links []ipLink
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return nil, fmt.Errorf("respuesta de 'ip' inesperada: %w", err)
	}
	var out []SourceInfo
	for _, l := range links {
		kind, detail, ok := classify(l)
		if !ok {
			continue
		}
		out = append(out, SourceInfo{Kind: kind, Iface: l.Ifname, Up: isUp(l), Detail: detail})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Iface < out[j].Iface
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func classify(l ipLink) (Source, string, bool) {
	infoKind := ""
	if l.LinkInfo != nil {
		infoKind = l.LinkInfo.InfoKind
	}
	switch {
	case infoKind == "wireguard":
		return SourceVPN, "wireguard", true
	case infoKind == "tun":
		return SourceVPN, "tun", true
	case isWireless(l.Ifname):
		return SourceWiFi, "wifi", true
	case infoKind == "" && hasDevice(l.Ifname):
		return SourceEth, "ethernet", true
	}
	return "", "", false
}

func isUp(l ipLink) bool {
	for _, f := range l.Flags {
		if f == "UP" {
			return true
		}
	}
	return l.Operstate == "up"
}

func isWireless(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name + "/wireless")
	return err == nil
}

func hasDevice(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name + "/device")
	return err == nil
}

// Resolve picks the best interface for a given source kind.
func Resolve(kind Source) (string, error) {
	srcs, err := DetectSources()
	if err != nil {
		return "", err
	}
	var cands []SourceInfo
	for _, s := range srcs {
		if s.Kind == kind {
			cands = append(cands, s)
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no se encontró ninguna interfaz para el origen %q", kind)
	}
	if kind == SourceEth || kind == SourceWiFi {
		if dev, _, err := defaultRoute(); err == nil {
			for _, c := range cands {
				if c.Iface == dev {
					return c.Iface, nil
				}
			}
		}
	}
	for _, c := range cands {
		if c.Up {
			return c.Iface, nil
		}
	}
	return cands[0].Iface, nil
}

// FirstWireless returns the wireless interface used to build the AP.
func FirstWireless() (string, error) {
	return Resolve(SourceWiFi)
}

func defaultRoute() (dev, gw string, err error) {
	raw, err := output("ip", "-j", "route", "show", "default")
	if err != nil {
		return "", "", err
	}
	var rs []ipRoute
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return "", "", err
	}
	if len(rs) == 0 {
		return "", "", fmt.Errorf("sin ruta por defecto")
	}
	return rs[0].Dev, rs[0].Gateway, nil
}

// gatewayFor returns the gateway of the default route via iface, if any.
func gatewayFor(iface string) string {
	raw, err := output("ip", "-j", "route", "show", "default", "dev", iface)
	if err != nil {
		return ""
	}
	var rs []ipRoute
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return ""
	}
	for _, r := range rs {
		if r.Gateway != "" {
			return r.Gateway
		}
	}
	return ""
}

// interfaceUp reports whether a named link is administratively up.
func interfaceUp(name string) bool {
	raw, err := output("ip", "-j", "link", "show", name)
	if err != nil {
		return false
	}
	var links []ipLink
	if err := json.Unmarshal([]byte(raw), &links); err != nil || len(links) == 0 {
		return false
	}
	return isUp(links[0])
}
