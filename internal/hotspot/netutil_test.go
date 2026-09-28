package hotspot

import (
	"strings"
	"testing"
)

func TestBuildNetPlanDefault(t *testing.T) {
	np, err := BuildNetPlan("10.42.42.0/24")
	if err != nil {
		t.Fatalf("BuildNetPlan: %v", err)
	}
	cases := map[string]string{
		"gateway":   np.Gateway.String(),
		"start":     np.DHCPStart.String(),
		"end":       np.DHCPEnd.String(),
		"broadcast": np.Broadcast.String(),
		"mask":      np.Mask,
		"gwcidr":    np.GatewayCIDR(),
	}
	want := map[string]string{
		"gateway":   "10.42.42.1",
		"start":     "10.42.42.10",
		"end":       "10.42.42.254",
		"broadcast": "10.42.42.255",
		"mask":      "255.255.255.0",
		"gwcidr":    "10.42.42.1/24",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s = %q, want %q", k, got, want[k])
		}
	}
}

func TestBuildNetPlanInvalid(t *testing.T) {
	for _, in := range []string{"nope", "10.0.0.0/31", "fd00::/64"} {
		if _, err := BuildNetPlan(in); err == nil {
			t.Errorf("BuildNetPlan(%q) = nil error, want error", in)
		}
	}
}

func TestNftRuleset(t *testing.T) {
	s := nftRuleset("hspot0", "8nternational", "10.42.42.0/24", 1360)
	for _, want := range []string{
		"table inet hotspot", "hspot0", "8nternational",
		"masquerade", "maxseg size set 1360", "10.42.42.0/24",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("nftRuleset missing %q", want)
		}
	}
}

func TestParseSource(t *testing.T) {
	for _, in := range []string{"eth", "wifi", "vpn", " VPN "} {
		if _, err := ParseSource(in); err != nil {
			t.Errorf("ParseSource(%q) = %v", in, err)
		}
	}
	if _, err := ParseSource("bogus"); err == nil {
		t.Error("ParseSource(bogus) = nil error, want error")
	}
}

func TestDerivedMAC(t *testing.T) {
	got, err := derivedMAC("f8:89:d2:37:93:f1")
	if err != nil {
		t.Fatalf("derivedMAC: %v", err)
	}
	if got == "f8:89:d2:37:93:f1" {
		t.Error("derived MAC must differ from the base MAC")
	}
	// locally administered bit set
	if got[:2] != "fa" {
		t.Errorf("derived MAC %q does not set the locally-administered bit", got)
	}
	if _, err := derivedMAC("not-a-mac"); err == nil {
		t.Error("derivedMAC(invalid) = nil error, want error")
	}
}

func TestDnsmasqConfigMTU(t *testing.T) {
	np, err := BuildNetPlan("10.42.42.0/24")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{APName: "hspot0", DNS: "8.8.8.8", MTU: 1420}
	if s := dnsmasqConfig(cfg, np); !strings.Contains(s, "dhcp-option=26,1420") {
		t.Errorf("expected MTU option in dnsmasq config:\n%s", s)
	}
	cfg.MTU = 1500
	if s := dnsmasqConfig(cfg, np); strings.Contains(s, "dhcp-option=26") {
		t.Error("should not advertise MTU 1500")
	}
}

func TestHostapdConfigOpen(t *testing.T) {
	cfg := &Config{APName: "hspot0", SSID: "Test", Band: "2.4", Open: true}
	s := hostapdConfig(cfg, 6)
	if !strings.Contains(s, "ssid=Test") || !strings.Contains(s, "hw_mode=g") {
		t.Errorf("unexpected hostapd config:\n%s", s)
	}
	if strings.Contains(s, "wpa_passphrase") {
		t.Error("open network must not contain wpa_passphrase")
	}
}

func TestHostapdConfigWPA2(t *testing.T) {
	cfg := &Config{APName: "hspot0", SSID: "Test", Band: "5", Pass: "secret12345"}
	s := hostapdConfig(cfg, 36)
	for _, want := range []string{"hw_mode=a", "wpa=2", "wpa_passphrase=secret12345", "rsn_pairwise=CCMP"} {
		if !strings.Contains(s, want) {
			t.Errorf("hostapdConfig missing %q", want)
		}
	}
}
