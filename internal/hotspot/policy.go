package hotspot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClientPolicy is the persisted per-device policy: its friendly name and the
// two switches (internet access and association permission).
type ClientPolicy struct {
	Name     string `json:"name,omitempty"`
	Internet bool   `json:"internet"`
	Allowed  bool   `json:"allowed"`
}

// Policy is the on-disk document mapping MACs to their policy.
type Policy struct {
	Clients map[string]ClientPolicy `json:"clients"`
}

// LoadPolicy reads the persisted policy, returning an empty one when the file
// does not exist yet.
func LoadPolicy() (*Policy, error) {
	return loadPolicyFrom(policyPath)
}

// loadPolicyFrom is the testable variant of LoadPolicy with an explicit path.
func loadPolicyFrom(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Policy{Clients: map[string]ClientPolicy{}}, nil
		}
		return nil, err
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("política corrupta en %s: %w", path, err)
	}
	if p.Clients == nil {
		p.Clients = map[string]ClientPolicy{}
	}
	return &p, nil
}

// Save writes the policy atomically to policyPath.
func (p *Policy) Save() error {
	return p.saveTo(policyPath)
}

// saveTo is the testable variant of Save with an explicit path. It writes a
// temp file in the same directory and renames it over the target so a crash
// never leaves a truncated file.
func (p *Policy) saveTo(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if p.Clients == nil {
		p.Clients = map[string]ClientPolicy{}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".clients-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Chmod(path, 0o600)
}

// Get returns the policy for a MAC, defaulting to allowed with internet.
func (p *Policy) Get(mac string) ClientPolicy {
	if cp, ok := p.Clients[strings.ToLower(strings.TrimSpace(mac))]; ok {
		return cp
	}
	return ClientPolicy{Internet: true, Allowed: true}
}

// Set stores the policy for a MAC, normalizing the key to lowercase.
func (p *Policy) Set(mac string, cp ClientPolicy) {
	if p.Clients == nil {
		p.Clients = map[string]ClientPolicy{}
	}
	p.Clients[strings.ToLower(strings.TrimSpace(mac))] = cp
}

// SetInternet enables or cuts off internet access for a device, updating both
// the persisted policy and the nft set.
func SetInternet(target string, on bool) error {
	mac, err := resolveMAC(target)
	if err != nil {
		return err
	}
	p, err := LoadPolicy()
	if err != nil {
		return err
	}
	cp := p.Get(mac)
	cp.Internet = on
	p.Set(mac, cp)
	if err := p.Save(); err != nil {
		return fmt.Errorf("no se pudo guardar la política: %w", err)
	}
	if on {
		if err := nftUnblock(mac); err != nil {
			return fmt.Errorf("no se pudo restablecer internet para %s: %w", mac, err)
		}
	} else {
		if err := nftBlock(mac); err != nil {
			return fmt.Errorf("no se pudo cortar internet a %s: %w", mac, err)
		}
	}
	return nil
}

// SetAllowed allows or denies association for a device, updating both the
// persisted policy and hostapd.
func SetAllowed(target string, on bool) error {
	mac, err := resolveMAC(target)
	if err != nil {
		return err
	}
	p, err := LoadPolicy()
	if err != nil {
		return err
	}
	cp := p.Get(mac)
	cp.Allowed = on
	p.Set(mac, cp)
	if err := p.Save(); err != nil {
		return fmt.Errorf("no se pudo guardar la política: %w", err)
	}
	if on {
		denied, err := hostapdDenied()
		if err != nil {
			return fmt.Errorf("no se pudo leer la lista de denegados: %w", err)
		}
		if !denied[mac] {
			return nil
		}
		if err := hostapdAllow(mac); err != nil {
			return fmt.Errorf("no se pudo permitir a %s: %w", mac, err)
		}
		return nil
	}
	if err := hostapdDeny(mac); err != nil {
		return fmt.Errorf("no se pudo denegar a %s: %w", mac, err)
	}
	if err := hostapdKick(mac); err != nil {
		return fmt.Errorf("no se pudo expulsar a %s: %w", mac, err)
	}
	return nil
}

// denyMACs renders the body of the deny file (MAC per line, sorted).
func denyMACs() (string, error) {
	p, err := LoadPolicy()
	if err != nil {
		return "", err
	}
	return p.denyMACsText(), nil
}

// denyMACsText returns the sorted newline-joined MACs that are not allowed,
// with a trailing newline when non-empty.
func (p *Policy) denyMACsText() string {
	var macs []string
	for mac, cp := range p.Clients {
		if !cp.Allowed {
			macs = append(macs, mac)
		}
	}
	if len(macs) == 0 {
		return ""
	}
	sort.Strings(macs)
	return strings.Join(macs, "\n") + "\n"
}

// Reconcile brings nft and hostapd in line with the persisted policy for the
// currently associated clients. It is a no-op when no hotspot is active.
func Reconcile() error {
	if _, err := LoadState(); err != nil {
		if errors.Is(err, errNoState) {
			return nil
		}
		return err
	}
	macs, err := associatedMACs()
	if err != nil {
		return fmt.Errorf("no se pudieron listar los clientes asociados: %w", err)
	}
	p, err := LoadPolicy()
	if err != nil {
		return err
	}
	blocked, err := nftBlocked()
	if err != nil {
		return fmt.Errorf("no se pudo leer la lista de bloqueados: %w", err)
	}
	denied, err := hostapdDenied()
	if err != nil {
		return fmt.Errorf("no se pudo leer la lista de denegados: %w", err)
	}
	for _, mac := range macs {
		cp := p.Get(mac)
		wantBlock := !cp.Internet
		if wantBlock != blocked[mac] {
			if wantBlock {
				if err := nftBlock(mac); err != nil {
					return fmt.Errorf("no se pudo cortar internet a %s: %w", mac, err)
				}
			} else {
				if err := nftUnblock(mac); err != nil {
					return fmt.Errorf("no se pudo restablecer internet para %s: %w", mac, err)
				}
			}
		}
		wantDeny := !cp.Allowed
		if wantDeny != denied[mac] {
			if wantDeny {
				if err := hostapdDeny(mac); err != nil {
					return fmt.Errorf("no se pudo denegar a %s: %w", mac, err)
				}
			} else {
				if err := hostapdAllow(mac); err != nil {
					return fmt.Errorf("no se pudo permitir a %s: %w", mac, err)
				}
			}
		}
	}
	return nil
}
