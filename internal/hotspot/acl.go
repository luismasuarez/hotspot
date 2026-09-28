package hotspot

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nftBlock inserts mac into the nft blocked set, cutting its internet access.
func nftBlock(mac string) error {
	if _, err := LoadState(); err != nil {
		return fmt.Errorf("hotspot inactivo: %w", err)
	}
	if err := runCmd("nft", "add", "element", "inet", "hotspot", blockedSet, "{", mac, "}"); err != nil {
		return fmt.Errorf("no se pudo bloquear %s: %w", mac, err)
	}
	return nil
}

// nftUnblock removes mac from the nft blocked set.
func nftUnblock(mac string) error {
	if _, err := LoadState(); err != nil {
		return fmt.Errorf("hotspot inactivo: %w", err)
	}
	if err := runCmd("nft", "delete", "element", "inet", "hotspot", blockedSet, "{", mac, "}"); err != nil {
		return fmt.Errorf("no se pudo desbloquear %s: %w", mac, err)
	}
	return nil
}

// nftBlocked returns the MACs currently present in the nft blocked set.
func nftBlocked() (map[string]bool, error) {
	if _, err := LoadState(); err != nil {
		return nil, fmt.Errorf("hotspot inactivo: %w", err)
	}
	raw, err := output("nft", "-j", "list", "set", "inet", "hotspot", blockedSet)
	if err != nil {
		// Table or set missing (hotspot not fully applied yet): treat as empty.
		return map[string]bool{}, nil
	}
	return parseNFTBlockedJSON(raw)
}

// nftSetJSON models the minimal shape of `nft -j list set` we care about.
type nftSetJSON struct {
	Nftables []struct {
		Set *struct {
			Elem []string `json:"elem"`
		} `json:"set"`
	} `json:"nftables"`
}

// parseNFTBlockedJSON extracts the set elements from `nft -j` output.
func parseNFTBlockedJSON(raw string) (map[string]bool, error) {
	out := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	var parsed nftSetJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	for _, obj := range parsed.Nftables {
		if obj.Set == nil {
			continue
		}
		for _, e := range obj.Set.Elem {
			if mac := normalizeMAC(e); mac != "" {
				out[mac] = true
			}
		}
	}
	return out, nil
}

// hostapdDeny adds mac to the running hostapd deny ACL.
func hostapdDeny(mac string) error {
	st, err := LoadState()
	if err != nil {
		return fmt.Errorf("hotspot inactivo: %w", err)
	}
	if err := runCmd("hostapd_cli", "-p", ctrlPath, "-i", st.APName, "deny_acl", "ADD_MAC", mac); err != nil {
		return fmt.Errorf("no se pudo añadir %s a la lista de bloqueo del AP: %w", mac, err)
	}
	return nil
}

// hostapdAllow removes mac from the running hostapd deny ACL.
func hostapdAllow(mac string) error {
	st, err := LoadState()
	if err != nil {
		return fmt.Errorf("hotspot inactivo: %w", err)
	}
	if err := runCmd("hostapd_cli", "-p", ctrlPath, "-i", st.APName, "deny_acl", "DEL_MAC", mac); err != nil {
		return fmt.Errorf("no se pudo quitar %s de la lista de bloqueo del AP: %w", mac, err)
	}
	return nil
}

// hostapdDenied returns the MACs currently present in the hostapd deny ACL.
func hostapdDenied() (map[string]bool, error) {
	st, err := LoadState()
	if err != nil {
		return nil, fmt.Errorf("hotspot inactivo: %w", err)
	}
	raw, err := output("hostapd_cli", "-p", ctrlPath, "-i", st.APName, "deny_acl", "SHOW")
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la lista de bloqueo del AP: %w", err)
	}
	return parseDenyACL(raw), nil
}

// hostapdKick forces mac to disconnect from the AP.
func hostapdKick(mac string) error {
	st, err := LoadState()
	if err != nil {
		return fmt.Errorf("hotspot inactivo: %w", err)
	}
	if err := runCmd("hostapd_cli", "-p", ctrlPath, "-i", st.APName, "deauthenticate", mac); err != nil {
		return fmt.Errorf("no se pudo expulsar a %s del AP: %w", mac, err)
	}
	return nil
}

// parseDenyACL parses `hostapd_cli deny_acl SHOW` output, keeping only valid
// MAC addresses and ignoring config lines such as "macaddr_acl=0", "FAIL" or
// blank lines. Each entry is "<mac>[ VLAN_ID=<n>]", so only the first field is
// considered.
func parseDenyACL(s string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if mac := normalizeMAC(fields[0]); mac != "" {
			out[mac] = true
		}
	}
	return out
}
