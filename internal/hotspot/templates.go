package hotspot

import (
	"fmt"
	"strings"
)

func hostapdConfig(cfg *Config, channel int) string {
	hwMode := "g"
	if cfg.Band == "5" {
		hwMode = "a"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "interface=%s\n", cfg.APName)
	b.WriteString("driver=nl80211\n")
	fmt.Fprintf(&b, "ssid=%s\n", cfg.SSID)
	fmt.Fprintf(&b, "hw_mode=%s\n", hwMode)
	fmt.Fprintf(&b, "channel=%d\n", channel)
	b.WriteString("ieee80211n=1\n")
	b.WriteString("wmm_enabled=1\n")
	b.WriteString("auth_algs=1\n")
	b.WriteString("ignore_broadcast_ssid=0\n")
	if !cfg.Open {
		fmt.Fprintf(&b, "wpa=2\n")
		fmt.Fprintf(&b, "wpa_passphrase=%s\n", cfg.Pass)
		b.WriteString("wpa_key_mgmt=WPA-PSK\n")
		b.WriteString("rsn_pairwise=CCMP\n")
	}
	return b.String()
}

func dnsmasqConfig(cfg *Config, np NetPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "interface=%s\n", cfg.APName)
	b.WriteString("bind-dynamic\n")
	b.WriteString("except-interface=lo\n")
	fmt.Fprintf(&b, "dhcp-range=%s,%s,%s,24h\n", np.DHCPStart, np.DHCPEnd, np.Mask)
	fmt.Fprintf(&b, "dhcp-option=3,%s\n", np.Gateway)
	fmt.Fprintf(&b, "dhcp-option=6,%s\n", np.Gateway)
	if cfg.MTU > 0 && cfg.MTU < 1500 {
		fmt.Fprintf(&b, "dhcp-option=26,%d\n", cfg.MTU)
	}
	fmt.Fprintf(&b, "server=%s\n", cfg.DNS)
	b.WriteString("no-resolv\n")
	b.WriteString("no-hosts\n")
	b.WriteString("domain-needed\n")
	b.WriteString("bogus-priv\n")
	return b.String()
}

func nftRuleset(ap, uplink, subnet string, mss int) string {
	return fmt.Sprintf(`table inet hotspot {
	chain forward {
		type filter hook forward priority filter; policy accept;
		iifname "%s" oifname "%s" tcp flags syn tcp option maxseg size set %d
	}
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		oifname "%s" ip saddr %s masquerade
	}
}
`, ap, uplink, mss, uplink, subnet)
}
