package hotspot

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// NetPlan describes the addressing of the hotspot subnet.
type NetPlan struct {
	Prefix    netip.Prefix
	Gateway   netip.Addr
	Mask      string
	DHCPStart netip.Addr
	DHCPEnd   netip.Addr
	Broadcast netip.Addr
}

// GatewayCIDR renders the gateway address with the subnet prefix length.
func (n NetPlan) GatewayCIDR() string {
	return fmt.Sprintf("%s/%d", n.Gateway, n.Prefix.Bits())
}

// BuildNetPlan validates cidr and derives gateway/DHCP range/broadcast.
func BuildNetPlan(cidr string) (NetPlan, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return NetPlan{}, fmt.Errorf("subred inválida %q: %w", cidr, err)
	}
	if !p.Addr().Is4() {
		return NetPlan{}, fmt.Errorf("solo se admite IPv4")
	}
	bits := p.Bits()
	if bits > 29 {
		return NetPlan{}, fmt.Errorf("la subred %s es demasiado pequeña (usa /29 o mayor)", cidr)
	}
	netw := p.Masked().Addr()
	bc := broadcastAddr(p)
	gw := netw.Next()
	start := offset(netw, 10)
	end := bc.Prev()
	if start.Compare(end) > 0 {
		start = offset(netw, 2)
		end = bc.Prev()
	}
	mask := net.IP(net.CIDRMask(bits, 32)).String()
	return NetPlan{
		Prefix:    p.Masked(),
		Gateway:   gw,
		Mask:      mask,
		DHCPStart: start,
		DHCPEnd:   end,
		Broadcast: bc,
	}, nil
}

func offset(a netip.Addr, n int) netip.Addr {
	for i := 0; i < n; i++ {
		a = a.Next()
	}
	return a
}

func broadcastAddr(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	bits := p.Bits()
	for i := 0; i < 4; i++ {
		b := i * 8
		switch {
		case bits <= b:
			a[i] = 0xFF
		case bits >= b+8:
		default:
			hostBits := uint(b + 8 - bits)
			a[i] |= byte((1 << hostBits) - 1)
		}
	}
	return netip.AddrFrom4(a)
}

// LocalPrefixes returns the IPv4 prefixes currently configured on the host.
func LocalPrefixes() ([]netip.Prefix, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			p, err := netip.ParsePrefix(ipn.String())
			if err != nil || !p.Addr().Is4() {
				continue
			}
			out = append(out, p.Masked())
		}
	}
	return out, nil
}

// Overlaps reports the first local prefix that overlaps p.
func Overlaps(p netip.Prefix, others []netip.Prefix) (netip.Prefix, bool) {
	for _, o := range others {
		if p.Overlaps(o) {
			return o, true
		}
	}
	return netip.Prefix{}, false
}

// derivedMAC returns a locally-administered unicast MAC distinct from base.
// A virtual AP interface on the same radio shares the base MAC by default,
// which makes the kernel reject "ip link set up" with ENOTUNIQ.
func derivedMAC(base string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(base))
	if err != nil {
		return "", fmt.Errorf("MAC inválida %q: %w", base, err)
	}
	mac := make(net.HardwareAddr, len(hw))
	copy(mac, hw)
	mac[0] = (mac[0] | 0x02) &^ 0x01
	if bytes.Equal(mac, hw) {
		mac[len(mac)-1] ^= 0x02
	}
	return mac.String(), nil
}

// wifiMAC reads the hardware address of an interface from sysfs.
func wifiMAC(iface string) string {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/address")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ifaceMTU reads an interface MTU from sysfs, defaulting to 1500.
func ifaceMTU(iface string) int {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/mtu")
	if err != nil {
		return 1500
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 1500
	}
	return n
}
