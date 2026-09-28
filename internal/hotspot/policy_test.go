package hotspot

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPolicyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	p := &Policy{Clients: map[string]ClientPolicy{
		"aa:bb:cc:dd:ee:01": {Name: "pixel", Internet: false, Allowed: true},
		"aa:bb:cc:dd:ee:02": {Internet: true, Allowed: false},
	}}
	if err := p.saveTo(path); err != nil {
		t.Fatalf("saveTo: %v", err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	got, err := loadPolicyFrom(path)
	if err != nil {
		t.Fatalf("loadPolicyFrom: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Errorf("round trip = %+v, want %+v", got, p)
	}
}

func TestLoadPolicyMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "clients.json")
	p, err := loadPolicyFrom(path)
	if err != nil {
		t.Fatalf("loadPolicyFrom missing: %v", err)
	}
	if p.Clients == nil || len(p.Clients) != 0 {
		t.Errorf("missing file = %+v, want empty policy", p)
	}
}

func TestLoadPolicyCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPolicyFrom(path); err == nil {
		t.Error("loadPolicyFrom(corrupt) = nil error, want error")
	}
}

func TestPolicyGetDefaults(t *testing.T) {
	p := &Policy{}
	cp := p.Get("AA:BB:CC:DD:EE:01")
	if !cp.Internet || !cp.Allowed {
		t.Errorf("Get default = %+v, want internet+allowed", cp)
	}
}

func TestPolicySetNormalizesKey(t *testing.T) {
	p := &Policy{}
	p.Set("AA:BB:CC:DD:EE:01", ClientPolicy{Name: "x", Internet: false, Allowed: false})
	if _, ok := p.Clients["aa:bb:cc:dd:ee:01"]; !ok {
		t.Fatalf("Set key not normalized: %+v", p.Clients)
	}
	cp := p.Get("aa:bb:cc:dd:ee:01")
	if cp.Internet || cp.Allowed || cp.Name != "x" {
		t.Errorf("Get after Set = %+v", cp)
	}
}

func TestDenyMACsText(t *testing.T) {
	p := &Policy{Clients: map[string]ClientPolicy{
		"aa:bb:cc:dd:ee:02": {Internet: true, Allowed: false},
		"aa:bb:cc:dd:ee:01": {Internet: true, Allowed: false},
		"aa:bb:cc:dd:ee:03": {Internet: false, Allowed: true},
	}}
	want := "aa:bb:cc:dd:ee:01\naa:bb:cc:dd:ee:02\n"
	if got := p.denyMACsText(); got != want {
		t.Errorf("denyMACsText = %q, want %q", got, want)
	}
	empty := &Policy{Clients: map[string]ClientPolicy{
		"aa:bb:cc:dd:ee:01": {Internet: true, Allowed: true},
	}}
	if got := empty.denyMACsText(); got != "" {
		t.Errorf("denyMACsText(empty) = %q, want \"\"", got)
	}
}
