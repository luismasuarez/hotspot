package hotspot

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings remembers the last SSID/passphrase chosen in the TUI so a restart
// reuses them instead of generating a fresh random password. A changing
// passphrase makes the profile saved on each client stale, which shows up as
// the phone repeatedly failing to associate.
type Settings struct {
	SSID string `json:"ssid,omitempty"`
	Pass string `json:"pass,omitempty"`
	Open bool   `json:"open,omitempty"`
}

// LoadSettings reads the persisted TUI settings, returning the zero value when
// the file is missing or unreadable.
func LoadSettings() Settings {
	var s Settings
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

// SaveSettings writes the TUI settings with 0600 perms, atomically enough for
// our purposes (same directory, single small file).
func SaveSettings(s Settings) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(settingsPath, 0o600)
}
