package hotspot

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStationDump(t *testing.T) {
	in := `Station aa:bb:cc:dd:ee:01 (on hspot0)
	inactive time:	120 ms
	rx bytes:	1234
	tx bytes:	5678
	signal:  	-45 dBm
	connected time:	60 seconds
Station AA:BB:CC:DD:EE:02 (on hspot0)
	signal:  -50 [-55, -60] dBm
	rx bytes:	99
	tx bytes:	100
	connected time:	5 seconds
`
	want := []stationInfo{
		{MAC: "aa:bb:cc:dd:ee:01", Signal: -45, RxBytes: 1234, TxBytes: 5678, Connected: 60 * time.Second},
		{MAC: "aa:bb:cc:dd:ee:02", Signal: -50, RxBytes: 99, TxBytes: 100, Connected: 5 * time.Second},
	}
	got := parseStationDump(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStationDump() = %+v, want %+v", got, want)
	}
}

func TestParseStationDumpEmpty(t *testing.T) {
	if got := parseStationDump(""); len(got) != 0 {
		t.Errorf("parseStationDump(\"\") = %+v, want empty", got)
	}
}

func TestParseLeases(t *testing.T) {
	in := `1700000000 aa:bb:cc:dd:ee:01 10.42.42.20 pixel 01:aa:bb:cc:dd:ee:01
1700000001 AA:BB:CC:DD:EE:02 10.42.42.21 * 01:aa:bb:cc:dd:ee:02

basura
1700000002 not-a-mac 10.42.42.22 otro x
`
	want := []lease{
		{MAC: "aa:bb:cc:dd:ee:01", IP: "10.42.42.20", Hostname: "pixel"},
		{MAC: "aa:bb:cc:dd:ee:02", IP: "10.42.42.21", Hostname: ""},
	}
	got := parseLeases(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLeases() = %+v, want %+v", got, want)
	}
}

func TestParseNeighbors(t *testing.T) {
	in := `[{"dst":"10.42.42.20","dev":"hspot0","lladdr":"aa:bb:cc:dd:ee:01","state":["REACHABLE"]},
	{"dst":"10.42.42.21","dev":"hspot0","lladdr":"AA:BB:CC:DD:EE:02","state":["STALE"]},
	{"dst":"10.42.42.22","dev":"hspot0","lladdr":"aa:bb:cc:dd:ee:03","state":["INCOMPLETE"]},
	{"dst":"10.42.42.23","dev":"hspot0","state":["FAILED"]}]`
	want := map[string]string{
		"10.42.42.20": "aa:bb:cc:dd:ee:01",
		"10.42.42.21": "aa:bb:cc:dd:ee:02",
	}
	got, err := parseNeighbors(in)
	if err != nil {
		t.Fatalf("parseNeighbors: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNeighbors() = %+v, want %+v", got, want)
	}
}

func TestParseNeighborsEmpty(t *testing.T) {
	got, err := parseNeighbors("")
	if err != nil {
		t.Fatalf("parseNeighbors(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parseNeighbors(\"\") = %+v, want empty", got)
	}
	if _, err := parseNeighbors("{no json"); err == nil {
		t.Error("parseNeighbors(invalid) = nil error, want error")
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:ff"},
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff"},
		{"AABBCCDDEEFF", "aa:bb:cc:dd:ee:ff"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff"},
		{"aa:bb:cc:dd:ee", ""},
		{"aa:bb:cc:dd:ee:gg", ""},
		{"not-a-mac", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeMAC(c.in); got != c.want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"  pixel  ", "pixel"},
		{"pi\x00xel\x7f", "pixel"},
		{"a\tb\nc", "abc"},
		{strings.Repeat("a", 40), strings.Repeat("a", 32)},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeName(c.in); got != c.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
