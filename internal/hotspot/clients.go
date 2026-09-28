package hotspot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client is a device seen by the hotspot, merged from the AP station list,
// the DHCP leases and the neighbor table.
type Client struct {
	MAC       string
	IP        string
	Hostname  string
	Signal    int
	RxBytes   uint64
	TxBytes   uint64
	Connected time.Duration
	Internet  bool
	Allowed   bool
}

type stationInfo struct {
	MAC       string
	Signal    int
	RxBytes   uint64
	TxBytes   uint64
	Connected time.Duration
}

type lease struct {
	MAC      string
	IP       string
	Hostname string
}

type neighbor struct {
	Dst    string   `json:"dst"`
	Dev    string   `json:"dev"`
	LLAddr string   `json:"lladdr"`
	State  []string `json:"state"`
}

// ScanClients returns currently associated stations merged with DHCP leases,
// the neighbor table and the persisted per-device policy.
func ScanClients() ([]Client, error) {
	st, err := LoadState()
	if err != nil {
		if errors.Is(err, errNoState) {
			return []Client{}, nil
		}
		return nil, err
	}

	var stations []stationInfo
	if raw, err := output("iw", "dev", st.APName, "station", "dump"); err == nil {
		stations = parseStationDump(raw)
	}

	var leases []lease
	if b, err := os.ReadFile(leasePath); err == nil {
		leases = parseLeases(string(b))
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	neighbors := map[string]string{}
	if raw, err := output("ip", "-j", "neigh", "show", "dev", st.APName); err == nil {
		if byIP, err := parseNeighbors(raw); err == nil {
			neighbors = byIP
		}
	}

	byMAC := map[string]*Client{}
	get := func(mac string) *Client {
		c, ok := byMAC[mac]
		if !ok {
			c = &Client{MAC: mac}
			byMAC[mac] = c
		}
		return c
	}

	for _, s := range stations {
		c := get(s.MAC)
		c.Signal = s.Signal
		c.RxBytes = s.RxBytes
		c.TxBytes = s.TxBytes
		c.Connected = s.Connected
	}
	for _, l := range leases {
		if l.MAC == "" {
			continue
		}
		c := get(l.MAC)
		if c.IP == "" {
			c.IP = l.IP
		}
		if c.Hostname == "" {
			c.Hostname = sanitizeName(l.Hostname)
		}
	}
	for ip, mac := range neighbors {
		c, ok := byMAC[mac]
		if !ok || c.IP != "" {
			continue
		}
		c.IP = ip
	}

	var pol *Policy
	if p, err := LoadPolicy(); err == nil {
		pol = p
	}
	for _, c := range byMAC {
		cp := ClientPolicy{Internet: true, Allowed: true}
		if pol != nil {
			cp = pol.Get(c.MAC)
		}
		c.Internet = cp.Internet
		c.Allowed = cp.Allowed
	}

	out := make([]Client, 0, len(byMAC))
	for _, c := range byMAC {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aerr := netip.ParseAddr(out[i].IP)
		aj, ajerr := netip.ParseAddr(out[j].IP)
		aok, ajok := aerr == nil, ajerr == nil
		if aok && ajok {
			if ai != aj {
				return ai.Less(aj)
			}
		} else if aok != ajok {
			return aok
		}
		return out[i].MAC < out[j].MAC
	})
	return out, nil
}

// associatedMACs returns the MACs currently associated to the AP.
func associatedMACs() ([]string, error) {
	st, err := LoadState()
	if err != nil {
		if errors.Is(err, errNoState) {
			return nil, nil
		}
		return nil, err
	}
	raw, err := output("iw", "dev", st.APName, "station", "dump")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range parseStationDump(raw) {
		out = append(out, s.MAC)
	}
	return out, nil
}

// resolveMAC maps a MAC or an IPv4 address (from lease/neighbor table) to a
// normalized MAC (lowercase colon form). Returns an error if not found.
func resolveMAC(target string) (string, error) {
	if m := normalizeMAC(target); m != "" {
		return m, nil
	}
	ip := strings.TrimSpace(target)
	if b, err := os.ReadFile(leasePath); err == nil {
		for _, l := range parseLeases(string(b)) {
			if l.IP == ip && l.MAC != "" {
				return l.MAC, nil
			}
		}
	}
	if st, err := LoadState(); err == nil {
		if raw, err := output("ip", "-j", "neigh", "show", "dev", st.APName); err == nil {
			if byIP, err := parseNeighbors(raw); err == nil {
				if mac := byIP[ip]; mac != "" {
					return mac, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no se encontró ningún dispositivo con %q", target)
}

// normalizeMAC lowercases and canonicalizes a MAC, tolerating 12 hex chars with
// or without separators (':' or '-'). Returns "" if it cannot parse.
func normalizeMAC(s string) string {
	raw := strings.NewReplacer(":", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	if len(raw) != 12 {
		return ""
	}
	for _, r := range raw {
		if !isHexDigit(r) {
			return ""
		}
	}
	return raw[0:2] + ":" + raw[2:4] + ":" + raw[4:6] + ":" + raw[6:8] + ":" + raw[8:10] + ":" + raw[10:12]
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// sanitizeName strips control characters and trims an untrusted hostname.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	out := []rune(strings.TrimSpace(b.String()))
	if len(out) > 32 {
		out = out[:32]
	}
	return string(out)
}

// parseStationDump parses the output of 'iw dev <iface> station dump'.
func parseStationDump(s string) []stationInfo {
	var out []stationInfo
	cur := -1
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "Station ") {
			fields := strings.Fields(t)
			if len(fields) >= 2 && normalizeMAC(fields[1]) != "" {
				out = append(out, stationInfo{MAC: normalizeMAC(fields[1])})
				cur = len(out) - 1
			} else {
				cur = -1
			}
			continue
		}
		if cur < 0 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "signal:"):
			if f := strings.Fields(strings.TrimPrefix(t, "signal:")); len(f) > 0 {
				if n, err := strconv.Atoi(f[0]); err == nil {
					out[cur].Signal = n
				}
			}
		case strings.HasPrefix(t, "rx bytes:"):
			out[cur].RxBytes = parseUintField(strings.TrimPrefix(t, "rx bytes:"))
		case strings.HasPrefix(t, "tx bytes:"):
			out[cur].TxBytes = parseUintField(strings.TrimPrefix(t, "tx bytes:"))
		case strings.HasPrefix(t, "connected time:"):
			if f := strings.Fields(strings.TrimPrefix(t, "connected time:")); len(f) > 0 {
				if n, err := strconv.Atoi(f[0]); err == nil {
					out[cur].Connected = time.Duration(n) * time.Second
				}
			}
		}
	}
	return out
}

func parseUintField(s string) uint64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.ParseUint(f[0], 10, 64)
	return n
}

// parseLeases parses a dnsmasq lease file: expiry mac ip hostname clientid.
func parseLeases(s string) []lease {
	var out []lease
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mac := normalizeMAC(fields[1])
		if mac == "" {
			continue
		}
		l := lease{MAC: mac, IP: fields[2]}
		if len(fields) >= 4 && fields[3] != "*" {
			l.Hostname = fields[3]
		}
		out = append(out, l)
	}
	return out
}

// parseNeighbors parses 'ip -j neigh' JSON into an IP -> MAC map, skipping
// entries without a link-layer address or in INCOMPLETE/FAILED state.
func parseNeighbors(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	var ns []neighbor
	if err := json.Unmarshal([]byte(raw), &ns); err != nil {
		return nil, err
	}
	for _, n := range ns {
		if n.LLAddr == "" {
			continue
		}
		skip := false
		for _, st := range n.State {
			if st == "INCOMPLETE" || st == "FAILED" {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		mac := normalizeMAC(n.LLAddr)
		if mac == "" {
			continue
		}
		out[strings.TrimSpace(n.Dst)] = mac
	}
	return out, nil
}
