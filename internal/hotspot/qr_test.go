package hotspot

import (
	"strings"
	"testing"
)

func TestRenderQR(t *testing.T) {
	var b strings.Builder
	if err := renderQR(&b, wifiPayload("MiRed", "secret123", false, false), false); err != nil {
		t.Fatalf("renderQR: %v", err)
	}
	if b.Len() == 0 {
		t.Fatal("renderQR produced empty output")
	}
}

func TestWifiPayload(t *testing.T) {
	cases := []struct {
		name   string
		ssid   string
		pass   string
		open   bool
		hidden bool
		want   string
	}{
		{"wpa", "MiRed", "secret123", false, false, `WIFI:T:WPA;S:MiRed;P:secret123;H:false;;`},
		{"open", "MiRed", "", true, false, `WIFI:T:nopass;S:MiRed;H:false;;`},
		{"hidden", "MiRed", "secret123", false, true, `WIFI:T:WPA;S:MiRed;P:secret123;H:true;;`},
	}
	for _, c := range cases {
		if got := wifiPayload(c.ssid, c.pass, c.open, c.hidden); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEscapeWifi(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a;b", `a\;b`},
		{"a,b:c", `a\,b\:c`},
		{`say "hi"`, `say \"hi\"`},
		{`back\slash`, `back\\slash`},
		{";", `\;`},
		{":", `\:`},
		{",", `\,`},
		{`"`, `\"`},
		{`\`, `\\`},
	}
	for _, c := range cases {
		if got := escapeWifi(c.in); got != c.want {
			t.Errorf("escapeWifi(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
