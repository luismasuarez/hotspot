package hotspot

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Up starts the hotspot described by cfg.
func Up(cfg *Config, r *Runner) error {
	if stateExists() {
		return fmt.Errorf("ya hay un hotspot activo (estado en %s); ejecuta 'hotspot down' primero", statePath)
	}
	np, err := BuildNetPlan(cfg.Subnet)
	if err != nil {
		return err
	}
	problems := preflight(cfg, np)
	if len(problems) > 0 {
		if !cfg.DryRun {
			return fmt.Errorf("preflight: %s", strings.Join(problems, "; "))
		}
		for _, p := range problems {
			fmt.Fprintf(r.Out, "aviso (dry-run): %s\n", p)
		}
	}

	prevForward, _ := readIPForward()
	sil := r.silent()

	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	fail := func(err error) error {
		fmt.Fprintf(r.Out, "\nerror: %v\nrevirtiendo cambios...\n", err)
		rollback()
		if !cfg.DryRun {
			removeState()
		}
		return err
	}

	fmt.Fprintf(r.Out, "levantando hotspot (origen=%s, uplink=%s, ap=%s)\n", cfg.Source, cfg.Uplink, cfg.APName)

	if err := r.writeFile(hostapdConf, hostapdConfig(cfg, cfg.Channel), 0o600); err != nil {
		return fail(err)
	}
	if err := r.writeFile(dnsmasqConf, dnsmasqConfig(cfg, np), 0o600); err != nil {
		return fail(err)
	}

	wifiWasUp := interfaceUp(cfg.WiFiIface)
	wasManaged := nmManaged(cfg.WiFiIface)
	if err := r.Run("", "nmcli", "dev", "set", cfg.WiFiIface, "managed", "no"); err != nil {
		return fail(err)
	}
	if wasManaged {
		undo = append(undo, func() {
			_ = sil.Run("", "nmcli", "dev", "set", cfg.WiFiIface, "managed", "yes")
		})
	}
	if err := r.Run("", "ip", "link", "set", cfg.WiFiIface, "up"); err != nil {
		return fail(err)
	}
	if !wifiWasUp {
		undo = append(undo, func() { _ = sil.Run("", "ip", "link", "set", cfg.WiFiIface, "down") })
	}

	if cfg.Dedicated {
		if err := r.Run("", "iw", "dev", cfg.WiFiIface, "interface", "add", cfg.APName, "type", "__ap"); err != nil {
			return fail(err)
		}
		undo = append(undo, func() { _ = sil.Run("", "iw", "dev", cfg.APName, "del") })
		if mac := wifiMAC(cfg.WiFiIface); mac != "" {
			if dm, err := derivedMAC(mac); err == nil {
				if err := r.Run("", "ip", "link", "set", "dev", cfg.APName, "address", dm); err != nil {
					fmt.Fprintf(r.Out, "aviso: no se pudo asignar MAC propia a %s: %v\n", cfg.APName, err)
				}
			}
		}
	}

	if err := r.Run("", "ip", "link", "set", cfg.APName, "up"); err != nil {
		return fail(err)
	}
	if err := r.Run("", "ip", "addr", "add", np.GatewayCIDR(), "dev", cfg.APName); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "ip", "addr", "del", np.GatewayCIDR(), "dev", cfg.APName) })

	if err := addUplinkRoute(r, cfg, np.Prefix.String()); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "ip", "route", "flush", "table", strconv.Itoa(cfg.Table)) })

	ruleArgs := []string{"rule", "add", "priority", strconv.Itoa(cfg.Priority), "from", np.Prefix.String(), "lookup", strconv.Itoa(cfg.Table)}
	ruleDelArgs := []string{"rule", "del", "priority", strconv.Itoa(cfg.Priority), "from", np.Prefix.String(), "lookup", strconv.Itoa(cfg.Table)}
	if err := r.Run("", "ip", ruleArgs...); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "ip", ruleDelArgs...) })

	if err := r.Run(nftRuleset(cfg.APName, cfg.Uplink, np.Prefix.String(), cfg.MSS), "nft", "-f", "-"); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "nft", "delete", "table", "inet", "hotspot") })

	// Forwarding: Docker sets `iptables -P FORWARD DROP`; a nft chain with
	// `policy accept` cannot override another base chain's DROP, so insert an
	// explicit ACCEPT into DOCKER-USER (Docker preserves it) or FORWARD.
	fwChain := detectFWChain()
	insOut := []string{"-I", fwChain, "1", "-i", cfg.APName, "-o", cfg.Uplink, "-j", "ACCEPT"}
	delOut := []string{"-D", fwChain, "-i", cfg.APName, "-o", cfg.Uplink, "-j", "ACCEPT"}
	insRet := []string{"-I", fwChain, "1", "-i", cfg.Uplink, "-o", cfg.APName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}
	delRet := []string{"-D", fwChain, "-i", cfg.Uplink, "-o", cfg.APName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}
	if err := r.Run("", "iptables", insOut...); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "iptables", delOut...) })
	if err := r.Run("", "iptables", insRet...); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "iptables", delRet...) })

	if err := r.Run("", "sysctl", "-qw", "net.ipv4.ip_forward=1"); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { _ = sil.Run("", "sysctl", "-qw", "net.ipv4.ip_forward="+prevForward) })

	if err := r.Run("", "hostapd", "-B", "-P", hostapdPID, hostapdConf); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { killByPIDFile(hostapdPID) })

	if err := r.Run("", "dnsmasq", "-C", dnsmasqConf, "-x", dnsmasqPID); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { killByPIDFile(dnsmasqPID) })

	st := &State{
		Source:        string(cfg.Source),
		Uplink:        cfg.Uplink,
		WiFiIface:     cfg.WiFiIface,
		APName:        cfg.APName,
		Subnet:        np.Prefix.String(),
		Table:         cfg.Table,
		RulePriority:  cfg.Priority,
		SSID:          cfg.SSID,
		Pass:          cfg.Pass,
		Open:          cfg.Open,
		Hidden:        cfg.Hidden,
		NMWasManaged:  wasManaged,
		PrevIPForward: prevForward,
		Dedicated:     cfg.Dedicated,
		GatewayCIDR:   np.GatewayCIDR(),
		FWChain:       fwChain,
		StartedAt:     time.Now(),
	}
	if cfg.DryRun {
		fmt.Fprintf(r.Out, "  + write %s (estado, mode 0644)\n", statePath)
	} else if err := st.Save(); err != nil {
		return fail(err)
	}

	fmt.Fprintf(r.Out, "\nhotspot activo\n")
	fmt.Fprintf(r.Out, "  ssid:    %s\n", cfg.SSID)
	fmt.Fprintf(r.Out, "  ap:      %s\n", cfg.APName)
	fmt.Fprintf(r.Out, "  uplink:  %s (%s)\n", cfg.Uplink, cfg.Source)
	fmt.Fprintf(r.Out, "  subred:  %s\n", np.Prefix)
	if !cfg.Open {
		fmt.Fprintf(r.Out, "  pass:    %s\n", cfg.Pass)
	} else {
		fmt.Fprintf(r.Out, "  pass:    (abierto, sin contraseña)\n")
	}
	printQR := !cfg.NoQR && !cfg.DryRun
	if cfg.QRPNG != "" && !cfg.DryRun {
		if err := writeQRPNG(cfg.QRPNG, wifiPayload(cfg.SSID, cfg.Pass, cfg.Open, cfg.Hidden), 512); err != nil {
			fmt.Fprintf(r.Err, "aviso: no se pudo guardar el PNG: %v\n", err)
		} else {
			fmt.Fprintf(r.Out, "  qr png:  %s\n", cfg.QRPNG)
		}
	}
	if printQR {
		fmt.Fprintln(r.Out, "\nescanea con el móvil para unirte:")
		if err := renderQR(r.Out, wifiPayload(cfg.SSID, cfg.Pass, cfg.Open, cfg.Hidden), cfg.QRInvert); err != nil {
			fmt.Fprintf(r.Err, "aviso: no se pudo generar el QR: %v\n", err)
		}
	}
	return nil
}

// detectFWChain returns the iptables chain where an ACCEPT overrides Docker's
// FORWARD DROP: DOCKER-USER when present, otherwise FORWARD.
func detectFWChain() string {
	if _, err := output("iptables", "-S", "DOCKER-USER"); err == nil {
		return "DOCKER-USER"
	}
	return "FORWARD"
}

func addUplinkRoute(r *Runner, cfg *Config, subnet string) error {
	tbl := strconv.Itoa(cfg.Table)
	// Keep the AP subnet reachable from our table, otherwise host->client
	// traffic (e.g. dnsmasq replies sourced from the AP IP 10.42.42.1) would
	// match the from-subnet rule and leak out through the uplink.
	if err := r.Run("", "ip", "route", "add", subnet, "dev", cfg.APName, "table", tbl); err != nil {
		return err
	}
	if cfg.Source == SourceVPN {
		return r.Run("", "ip", "route", "add", "default", "dev", cfg.Uplink, "table", tbl)
	}
	if gw := gatewayFor(cfg.Uplink); gw != "" {
		return r.Run("", "ip", "route", "add", "default", "via", gw, "dev", cfg.Uplink, "table", tbl)
	}
	return r.Run("", "ip", "route", "add", "default", "dev", cfg.Uplink, "table", tbl)
}

func preflight(cfg *Config, np NetPlan) []string {
	var problems []string
	if !cfg.DryRun && os.Geteuid() != 0 {
		problems = append(problems, "se requieren privilegios de root (ejecuta con sudo)")
	}
	for _, tool := range []string{"ip", "iw", "nft", "iptables", "hostapd", "dnsmasq"} {
		if !Exists(tool) {
			problems = append(problems, fmt.Sprintf("falta el ejecutable %q (sudo apt install %s)", tool, aptPackage(tool)))
		}
	}
	if _, err := net.InterfaceByName(cfg.Uplink); err != nil {
		problems = append(problems, fmt.Sprintf("la interfaz de origen %q no existe", cfg.Uplink))
	}
	if _, err := net.InterfaceByName(cfg.WiFiIface); err != nil {
		problems = append(problems, fmt.Sprintf("la interfaz wifi %q no existe", cfg.WiFiIface))
	}
	if !supportsAP() {
		problems = append(problems, fmt.Sprintf("no se detecta soporte de modo AP en %s", cfg.WiFiIface))
	}
	if !interfaceUp(cfg.Uplink) {
		problems = append(problems, fmt.Sprintf("la interfaz de origen %q está caída", cfg.Uplink))
	}
	if prefixes, err := LocalPrefixes(); err == nil {
		if other, ok := Overlaps(np.Prefix, prefixes); ok {
			problems = append(problems, fmt.Sprintf("la subred %s ya está en uso por %s (usa --subnet)", np.Prefix, other))
		}
	}
	return problems
}

func supportsAP() bool {
	raw, err := output("iw", "list")
	if err != nil {
		return false
	}
	return strings.Contains(raw, "* AP")
}

func readIPForward() (string, error) {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return "1", err
	}
	return strings.TrimSpace(string(b)), nil
}

func nmManaged(iface string) bool {
	raw, err := output("nmcli", "-t", "-g", "GENERAL.STATE", "device", "show", iface)
	if err != nil {
		return false
	}
	return !strings.Contains(raw, "unmanaged")
}

func killByPIDFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
	}
	_ = os.Remove(path)
}

func aptPackage(tool string) string {
	switch tool {
	case "ip":
		return "iproute2"
	case "nft":
		return "nftables"
	default:
		return tool
	}
}
