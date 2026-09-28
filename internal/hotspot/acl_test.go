package hotspot

import (
	"reflect"
	"testing"
)

func TestParseNFTBlockedJSON(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    map[string]bool
		wantErr bool
	}{
		{
			name: "single set with elements",
			in:   `{"nftables":[{"metainfo":{"version":"1.0.9"}},{"set":{"family":"inet","name":"blocked","table":"hotspot","type":"ether_addr","elem":["aa:bb:cc:dd:ee:01","AA:BB:CC:DD:EE:02"]}}]}`,
			want: map[string]bool{
				"aa:bb:cc:dd:ee:01": true,
				"aa:bb:cc:dd:ee:02": true,
			},
		},
		{
			name: "set without elements",
			in:   `{"nftables":[{"set":{"family":"inet","name":"blocked","elem":[]}}]}`,
			want: map[string]bool{},
		},
		{
			name: "no set object",
			in:   `{"nftables":[{"metainfo":{"version":"1.0.9"}}]}`,
			want: map[string]bool{},
		},
		{
			name: "skips invalid mac entries",
			in:   `{"nftables":[{"set":{"elem":["aa:bb:cc:dd:ee:01","basura"]}}]}`,
			want: map[string]bool{"aa:bb:cc:dd:ee:01": true},
		},
		{
			name: "empty input",
			in:   "",
			want: map[string]bool{},
		},
		{
			name:    "malformed json",
			in:      "{no json",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseNFTBlockedJSON(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseNFTBlockedJSON(%q) = nil error, want error", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNFTBlockedJSON(%q): %v", c.in, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseNFTBlockedJSON(%q) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestParseDenyACL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]bool
	}{
		{
			name: "config lines and macs",
			in: `macaddr_acl=0
accept_mac_file=
deny_mac_file=/run/hotspot/deny.mac
aa:bb:cc:dd:ee:01
AA:BB:CC:DD:EE:02`,
			want: map[string]bool{
				"aa:bb:cc:dd:ee:01": true,
				"aa:bb:cc:dd:ee:02": true,
			},
		},
		{
			name: "blank lines and failure",
			in:   "\nFAIL\n\n  \n",
			want: map[string]bool{},
		},
		{
			name: "empty input",
			in:   "",
			want: map[string]bool{},
		},
		{
			name: "dash separated mac",
			in:   "aa-bb-cc-dd-ee-ff\n",
			want: map[string]bool{"aa:bb:cc:dd:ee:ff": true},
		},
		{
			name: "real hostapd output with VLAN_ID suffix",
			in: `a2:9e:3d:f7:c5:08 VLAN_ID=0
AA:BB:CC:DD:EE:FF VLAN_ID=0`,
			want: map[string]bool{
				"a2:9e:3d:f7:c5:08": true,
				"aa:bb:cc:dd:ee:ff": true,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseDenyACL(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseDenyACL(%q) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}
