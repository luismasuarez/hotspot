package hotspot

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

var errNoState = errors.New("no hay un hotspot activo")

// State is persisted under /run/hotspot so 'down' can undo everything.
type State struct {
	Source        string    `json:"source"`
	Uplink        string    `json:"uplink"`
	WiFiIface     string    `json:"wifi_iface"`
	APName        string    `json:"ap_iface"`
	Subnet        string    `json:"subnet"`
	Table         int       `json:"route_table"`
	RulePriority  int       `json:"rule_priority"`
	SSID          string    `json:"ssid"`
	Pass          string    `json:"pass,omitempty"`
	Open          bool      `json:"open"`
	Hidden        bool      `json:"hidden"`
	NMWasManaged  bool      `json:"nm_was_managed"`
	PrevIPForward string    `json:"prev_ip_forward"`
	Dedicated     bool      `json:"dedicated_ap"`
	GatewayCIDR   string    `json:"gateway_cidr"`
	FWChain       string    `json:"fw_chain"`
	StartedAt     time.Time `json:"started_at"`
}

// Save writes the state file.
func (s *State) Save() error {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(statePath, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(statePath, 0o600)
}

// LoadState reads the persisted state, or errNoState when inactive.
func LoadState() (*State, error) {
	b, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNoState
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func stateExists() bool {
	_, err := os.Stat(statePath)
	return err == nil
}

func removeState() {
	_ = os.Remove(statePath)
	_ = os.Remove(hostapdConf)
	_ = os.Remove(hostapdPID)
	_ = os.Remove(dnsmasqConf)
	_ = os.Remove(dnsmasqPID)
	_ = os.Remove(runDir)
}
