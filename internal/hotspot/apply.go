package hotspot

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
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

	// Clear any leftover daemon/AP from a previous session whose state file is
	// gone (e.g. after a reboot or a crashed run). Otherwise hostapd fails to
	// reconfigure an interface already held in AP mode ("Could not configure
	// driver mode"/segfault).
	if !cfg.DryRun {
		stopDaemons(r)
	}

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

	// hostapd reads the deny MAC list at startup, so seed it from the
	// persisted policy before the daemon starts (devices blocked in a previous
	// session are refused from the first association).
	denied, err := denyMACs()
	if err != nil {
		return fail(err)
	}
	if err := r.writeFile(denyMACPath, denied, 0o600); err != nil {
		return fail(err)
	}

	wasManaged := nmManaged(cfg.WiFiIface)
	if err := r.Run("", "nmcli", "dev", "set", cfg.WiFiIface, "managed", "no"); err != nil {
		return fail(err)
	}
	if wasManaged {
		undo = append(undo, func() {
			_ = sil.Run("", "nmcli", "dev", "set", cfg.WiFiIface, "managed", "yes")
		})
	}
	if !cfg.DryRun {
		resetWiFiDriver(r, cfg.WiFiIface)
	}
	wifiWasUp := interfaceUp(cfg.WiFiIface)
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
	if err := ensureGatewayAddr(r, cfg.APName, np.GatewayCIDR()); err != nil {
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

	// hostapd -B forks before configuring the AP, so its stdout is lost and a
	// startup failure would otherwise be silent. Keep a log (-f) and verify the
	// BSS actually came up; if not, fail 'up' and surface the log.
	if err := r.Run("", "hostapd", "-f", hostapdLog, "-B", "-P", hostapdPID, hostapdConf); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { killByPIDFile(hostapdPID) })
	_ = os.Chmod(hostapdLog, 0o600)
	if !cfg.DryRun {
		if err := waitAPUp(cfg); err != nil {
			if tail := hostapdLogTail(); tail != "" {
				fmt.Fprintf(r.Err, "\nlog de hostapd:\n%s\n", tail)
			}
			return fail(fmt.Errorf("el AP no quedó operativo: %w", err))
		}
		// Reloading the driver (rtw88 workaround) or bringing up the BSS can
		// flush the interface addresses; re-assert the gateway IP before
		// dnsmasq starts, or it has no address to serve DHCP from.
		if err := ensureGatewayAddr(r, cfg.APName, np.GatewayCIDR()); err != nil {
			return fail(err)
		}
	}

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

	// Re-apply the persisted per-device policy (internet / association) to the
	// freshly created nft set and hostapd ACL. Best effort: a failure here
	// must not tear down a working hotspot.
	if !cfg.DryRun {
		if err := Reconcile(); err != nil {
			fmt.Fprintf(r.Err, "aviso: no se pudo aplicar la política de dispositivos: %v\n", err)
		}
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

// resetWiFiDriver reloads the wireless driver when it is a Realtek rtw88 one.
// That driver degrades after a while in AP mode: clients authenticate but never
// complete association (they retry auth and send deauth reason 3). A module
// reload restores a clean firmware state. It is a no-op for other drivers and
// best-effort on failure, so it never blocks 'up'.
func resetWiFiDriver(r *Runner, iface string) {
	drv := driverName(iface)
	if !strings.HasPrefix(drv, "rtw88") {
		return
	}
	fmt.Fprintf(r.Out, "reiniciando driver %s (workaround modo AP)\n", drv)
	_ = r.Run("", "ip", "link", "set", iface, "down")
	if err := r.Run("", "modprobe", "-r", drv); err != nil {
		fmt.Fprintf(r.Err, "aviso: no se pudo descargar %s: %v\n", drv, err)
		return
	}
	if err := r.Run("", "modprobe", drv); err != nil {
		fmt.Fprintf(r.Err, "aviso: no se pudo recargar %s: %v\n", drv, err)
		return
	}
	// The interface disappears while the module is unloaded; wait for it back.
	for i := 0; i < 40; i++ {
		if _, err := os.Stat("/sys/class/net/" + iface); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(r.Err, "aviso: %s no reapareció tras recargar %s\n", iface, drv)
}

// driverName returns the kernel driver bound to iface (e.g. "rtw88_8822ce").
func driverName(iface string) string {
	link, err := os.Readlink("/sys/class/net/" + iface + "/device/driver")
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// waitAPUp polls the interface until the kernel reports it as an AP carrying
// our SSID. It fails early if the hostapd process died, converting the silent
// post-daemonize failure of `hostapd -B` into a visible error.
func waitAPUp(cfg *Config) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, err := readPID(hostapdPID); err == nil && !pidAlive(pid) {
			return fmt.Errorf("hostapd terminó durante el arranque")
		}
		if apReady(cfg.APName, cfg.SSID) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("%s no alcanzó el estado AP con ssid %q", cfg.APName, cfg.SSID)
}

// apReady reports whether `iw dev <ap> info` shows an AP BSS with ssid.
func apReady(ap, ssid string) bool {
	raw, err := output("iw", "dev", ap, "info")
	if err != nil {
		return false
	}
	typeAP, ssidOK := false, false
	for _, line := range strings.Split(raw, "\n") {
		switch strings.TrimSpace(line) {
		case "type AP":
			typeAP = true
		case "ssid " + ssid:
			ssidOK = true
		}
	}
	return typeAP && ssidOK
}

// ensureGatewayAddr adds cidr to iface unless it is already configured, so it
// is safe to call repeatedly (e.g. before and after starting hostapd).
func ensureGatewayAddr(r *Runner, iface, cidr string) error {
	if addressPresent(iface, cidr) {
		return nil
	}
	return r.Run("", "ip", "addr", "add", cidr, "dev", iface)
}

// addressPresent reports whether cidr is already assigned to iface.
func addressPresent(iface, cidr string) bool {
	raw, err := output("ip", "-o", "addr", "show", "dev", iface)
	if err != nil {
		return false
	}
	return strings.Contains(raw, cidr)
}

// readPID reads an integer PID from a file.
func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// pidAlive reports whether a process with pid exists.
func pidAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// hostapdLogTail returns the last lines of the hostapd log, for diagnosis.
func hostapdLogTail() string {
	b, err := os.ReadFile(hostapdLog)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	return strings.Join(lines, "\n")
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
	// Only signal if the process is really the daemon we launched; a reused PID
	// must never be killed.
	if p, err := os.FindProcess(pid); err == nil && procMatches(path, pid) {
		_ = p.Signal(syscall.SIGTERM)
	}
	_ = os.Remove(path)
}

// procMatches reports whether the process holding pid was started with conf as
// its first argument (e.g. "hostapd ... /run/hotspot/hostapd.conf"), so a stale
// PID file cannot take down an unrelated process.
func procMatches(conf string, pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(b), "\x00", " "), conf)
}

// killDaemons stops hostapd and dnsmasq by their PID files, tolerating a
// missing file by matching the freshly launched process via procMatches.
// It is used by 'up' before taking over the interface and by 'down'. Must run
// as root to see the processes.
func killDaemons() {
	killByPIDFile(hostapdPID)
	killByPIDFile(dnsmasqPID)
}

// stopDaemons forcefully stops the hotspot daemons. Besides the PID files it
// also matches by the run-dir config path, so an AP left over from a previous
// session whose PID file is gone (or was never written) cannot keep holding the
// interface in AP mode.
func stopDaemons(r *Runner) {
	killDaemons()
	_ = r.Run("", "pkill", "-f", hostapdConf)
	_ = r.Run("", "pkill", "-f", dnsmasqConf)
}

func aptPackage(tool string) string {
	switch tool {
	case "ip":
		return "iproute2"
	case "nft":
		return "nftables"
	case "hostapd_cli":
		return "hostapd"
	default:
		return tool
	}
}
