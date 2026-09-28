package hotspot

import (
	"fmt"
	"os"
)

// Check is a single diagnostic result.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Doctor runs read-only diagnostics about the host environment.
func Doctor() []Check {
	var checks []Check
	checks = append(checks, Check{"root", os.Geteuid() == 0, "se necesita sudo para aplicar cambios"})
	for _, tool := range []string{"ip", "iw", "nft", "hostapd", "dnsmasq"} {
		checks = append(checks, Check{"tool:" + tool, Exists(tool), "sudo apt install " + aptPackage(tool)})
	}
	checks = append(checks, Check{"modo AP", supportsAP(), "el chip wifi debe anunciar '* AP' en 'iw list'"})
	if f, err := readIPForward(); err == nil {
		checks = append(checks, Check{"ip_forward", f == "1", "forwarding IPv4 (valor=" + f + ")"})
	}
	if stateExists() {
		checks = append(checks, Check{"estado", false, "ya hay un hotspot activo (usa 'hotspot down')"})
	} else {
		checks = append(checks, Check{"estado", true, "sin hotspot activo"})
	}
	if srcs, err := DetectSources(); err == nil {
		checks = append(checks, Check{"fuentes", len(srcs) > 0, fmt.Sprintf("%d interfaces utilizables", len(srcs))})
	}
	return checks
}
