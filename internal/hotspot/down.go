package hotspot

import (
	"errors"
	"fmt"
	"strconv"
)

// Down stops an active hotspot and reverses every change.
func Down(r *Runner) error {
	st, err := LoadState()
	if err != nil {
		if errors.Is(err, errNoState) {
			// No state file, but a daemon from a crashed/rebooted session may
			// still hold the AP interface. Clean it up before reporting idle.
			stopDaemons(r)
			return err
		}
		return err
	}
	fmt.Fprintln(r.Out, "deteniendo hotspot...")
	stopDaemons(r)
	_ = r.Run("", "nft", "delete", "table", "inet", "hotspot")
	if st.APName != "" && st.Uplink != "" {
		chain := st.FWChain
		if chain == "" {
			chain = "FORWARD"
		}
		_ = r.Run("", "iptables", "-D", chain, "-i", st.APName, "-o", st.Uplink, "-j", "ACCEPT")
		_ = r.Run("", "iptables", "-D", chain, "-i", st.Uplink, "-o", st.APName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
	}
	_ = r.Run("", "ip", "rule", "del", "priority", strconv.Itoa(st.RulePriority), "from", st.Subnet, "lookup", strconv.Itoa(st.Table))
	_ = r.Run("", "ip", "route", "flush", "table", strconv.Itoa(st.Table))
	if st.GatewayCIDR != "" && st.APName != "" {
		_ = r.Run("", "ip", "addr", "del", st.GatewayCIDR, "dev", st.APName)
	}
	if st.Dedicated && st.APName != "" {
		_ = r.Run("", "iw", "dev", st.APName, "del")
	}
	if st.NMWasManaged && st.WiFiIface != "" {
		_ = r.Run("", "nmcli", "dev", "set", st.WiFiIface, "managed", "yes")
	}
	if st.PrevIPForward != "" && st.PrevIPForward != "1" {
		_ = r.Run("", "sysctl", "-qw", "net.ipv4.ip_forward="+st.PrevIPForward)
	}
	removeState()
	fmt.Fprintln(r.Out, "hotspot detenido")
	return nil
}
