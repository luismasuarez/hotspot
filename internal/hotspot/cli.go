package hotspot

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Main is the CLI entry point. It returns the process exit code.
func Main(args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		usage(out)
		return 2
	}
	switch args[0] {
	case "up":
		return cmdUp(args[1:], out, errw)
	case "down":
		return cmdDown(args[1:], out, errw)
	case "status":
		return cmdStatus(args[1:], out, errw)
	case "qr":
		return cmdQr(args[1:], out, errw)
	case "sources":
		return cmdSources(args[1:], out, errw)
	case "doctor":
		return cmdDoctor(args[1:], out, errw)
	case "version", "--version", "-v":
		fmt.Fprintf(out, "hotspot %s\n", Version())
		return 0
	case "tui":
		return cmdTui(args[1:], out, errw)
	case "clients":
		return cmdClients(args[1:], out, errw)
	case "block":
		return cmdBlock(args[1:], out, errw)
	case "unblock":
		return cmdUnblock(args[1:], out, errw)
	case "deny":
		return cmdDeny(args[1:], out, errw)
	case "allow":
		return cmdAllow(args[1:], out, errw)
	case "kick":
		return cmdKick(args[1:], out, errw)
	case "-h", "--help", "help":
		usage(out)
		return 0
	default:
		fmt.Fprintf(errw, "comando desconocido: %s\n\n", args[0])
		usage(errw)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `hotspot - crea un punto de acceso wifi que comparte un origen de internet

uso:
  hotspot <comando> [flags]

comandos:
  up        levanta el hotspot
  down      detiene el hotspot y revierte los cambios
  status    muestra el estado del hotspot
  qr        muestra el QR del hotspot activo
  sources   lista los orígenes de internet disponibles
  doctor    comprueba los requisitos del sistema
  version   muestra la versión del binario
  clients   lista los dispositivos conectados y su estado
  block     corta el internet a un dispositivo (MAC o IP)
  unblock   devuelve el internet a un dispositivo
  deny      impide que un dispositivo se conecte
  allow     permite que un dispositivo se conecte
  kick      desconecta a un dispositivo ahora
  tui       panel interactivo de gestión de dispositivos

orígenes (--source):
  eth       comparte el internet del ethernet principal
  wifi      comparte el internet de la wifi principal (mismo radio, mismo canal)
  vpn       comparte el internet de la VPN (por defecto)

ejemplo:
  sudo hotspot up --source vpn --ssid WIFI_GRATIS
  sudo hotspot up --source vpn --dry-run
  sudo hotspot down
`)
}

func cmdUp(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(errw)
	source := fs.String("source", "vpn", "origen de internet: eth | wifi | vpn")
	iface := fs.String("iface", "", "interfaz de origen (autodetectada si se omite)")
	ssid := fs.String("ssid", defaultSSID, "nombre del hotspot")
	pass := fs.String("pass", "", "contraseña WPA2 (mínimo 8 caracteres)")
	open := fs.Bool("open", false, "hotspot abierto, sin contraseña")
	band := fs.String("band", "2.4", "banda del AP: 2.4 | 5")
	channel := fs.Int("channel", 0, "canal del AP (0 = automático)")
	subnet := fs.String("subnet", defaultSubnet, "subred del hotspot en CIDR")
	dns := fs.String("dns", defaultDNS, "DNS entregado a los clientes")
	apName := fs.String("ap-iface", defaultAPName, "interfaz AP virtual a crear")
	table := fs.Int("table", defaultTable, "id de tabla de enrutado")
	priority := fs.Int("rule-priority", defaultPriority, "prioridad de la regla de enrutado")
	mss := fs.Int("mss", defaultMSS, "MSS máximo para clamping TCP")
	mtu := fs.Int("mtu", 0, "MTU anunciado a los clientes (0 = auto según el uplink)")
	dry := fs.Bool("dry-run", false, "solo mostrar los comandos, sin aplicarlos")
	hidden := fs.Bool("hidden", false, "red oculta (SSID no difundido)")
	noQR := fs.Bool("no-qr", false, "no imprimir el QR al levantar")
	qrInvert := fs.Bool("qr-invert", false, "invertir colores del QR (temas oscuros)")
	qrPNG := fs.String("qr-png", "", "guardar además el QR como PNG en esta ruta")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	kind, err := ParseSource(*source)
	if err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 2
	}
	if !*open && *pass != "" && len(*pass) < 8 {
		fmt.Fprintln(errw, "error: la contraseña WPA2 debe tener al menos 8 caracteres")
		return 2
	}
	if *band != "2.4" && *band != "5" {
		fmt.Fprintln(errw, "error: --band debe ser 2.4 o 5")
		return 2
	}

	r := NewRunner(*dry, out, errw)

	wifi, err := FirstWireless()
	if err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}

	uplink := *iface
	if uplink == "" {
		if kind == SourceWiFi {
			uplink = wifi
		} else if uplink, err = Resolve(kind); err != nil {
			fmt.Fprintln(errw, "error:", err)
			return 1
		}
	}

	ch := *channel
	if ch == 0 && kind == SourceWiFi {
		if c, cerr := currentWiFiChannel(wifi); cerr == nil {
			ch = c
			fmt.Fprintf(out, "aviso: comparte el radio wifi, el AP queda en el canal %d\n", ch)
		}
	}
	if ch == 0 {
		ch = defaultChannel(*band)
	}

	finalPass := *pass
	if !*open && finalPass == "" {
		finalPass = randomPassword(12)
	}

	apNameFinal := *apName
	if kind != SourceWiFi && apNameFinal == defaultAPName {
		apNameFinal = wifi
	}
	dedicated := apNameFinal != wifi
	if kind == SourceWiFi && !dedicated {
		fmt.Fprintln(errw, "error: --source wifi necesita una interfaz AP virtual distinta de la wifi (--ap-iface)")
		return 2
	}

	advMTU := *mtu
	if advMTU == 0 {
		advMTU = ifaceMTU(uplink)
	}

	cfg := &Config{
		Source:    kind,
		Uplink:    uplink,
		WiFiIface: wifi,
		SSID:      *ssid,
		Pass:      finalPass,
		Open:      *open,
		Hidden:    *hidden,
		Band:      *band,
		Channel:   ch,
		Subnet:    *subnet,
		DNS:       *dns,
		APName:    apNameFinal,
		Table:     *table,
		Priority:  *priority,
		MSS:       *mss,
		MTU:       advMTU,
		DryRun:    *dry,
		Dedicated: dedicated,
		NoQR:      *noQR,
		QRInvert:  *qrInvert,
		QRPNG:     *qrPNG,
	}
	if err := Up(cfg, r); err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	return 0
}

func cmdDown(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(errw)
	dry := fs.Bool("dry-run", false, "solo mostrar los comandos")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	r := NewRunner(*dry, out, errw)
	if err := Down(r); err != nil {
		if errors.Is(err, errNoState) {
			fmt.Fprintln(out, "hotspot: inactivo")
			return 0
		}
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	return 0
}

func cmdStatus(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := LoadState()
	if err != nil {
		fmt.Fprintln(out, "hotspot: inactivo")
		return 1
	}
	fmt.Fprintln(out, "hotspot: activo")
	fmt.Fprintf(out, "  ssid:    %s\n", st.SSID)
	fmt.Fprintf(out, "  ap:      %s\n", st.APName)
	fmt.Fprintf(out, "  uplink:  %s (%s)\n", st.Uplink, st.Source)
	fmt.Fprintf(out, "  subred:  %s\n", st.Subnet)
	fmt.Fprintf(out, "  desde:   %s\n", st.StartedAt.Format(time.RFC3339))
	if raw, err := output("iw", "dev", st.APName, "station", "dump"); err == nil {
		fmt.Fprintf(out, "  clientes: %d\n", strings.Count(raw, "Station "))
	}
	return 0
}

func cmdQr(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	fs.SetOutput(errw)
	invert := fs.Bool("invert", false, "invertir colores del QR (temas oscuros)")
	png := fs.String("png", "", "guardar además el QR como PNG en esta ruta")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := LoadState()
	if err != nil {
		fmt.Fprintln(out, "hotspot: inactivo")
		return 1
	}
	if !st.Open && st.Pass == "" {
		fmt.Fprintln(errw, "aviso: la contraseña no está guardada (hotspot levantado con una versión anterior); reinicia con 'sudo hotspot down && sudo hotspot up'")
	}
	payload := wifiPayload(st.SSID, st.Pass, st.Open, st.Hidden)
	fmt.Fprintf(out, "ssid: %s\n", st.SSID)
	if st.Open {
		fmt.Fprintln(out, "pass: (abierta, sin contraseña)")
	} else {
		fmt.Fprintf(out, "pass: %s\n", st.Pass)
	}
	if *png != "" {
		if err := writeQRPNG(*png, payload, 512); err != nil {
			fmt.Fprintln(errw, "error:", err)
			return 1
		}
		fmt.Fprintf(out, "png:  %s\n", *png)
	}
	if err := renderQR(out, payload, *invert); err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	return 0
}

func cmdSources(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("sources", flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	srcs, err := DetectSources()
	if err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	if len(srcs) == 0 {
		fmt.Fprintln(out, "no se detectaron orígenes de internet")
		return 1
	}
	fmt.Fprintf(out, "%-8s %-16s %-4s %s\n", "ORIGEN", "INTERFAZ", "UP", "TIPO")
	for _, s := range srcs {
		fmt.Fprintf(out, "%-8s %-16s %-4s %s\n", s.Kind, s.Iface, yesno(s.Up), s.Detail)
	}
	return 0
}

func cmdDoctor(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	failed := 0
	for _, c := range Doctor() {
		mark := "ok"
		if !c.OK {
			mark = "KO"
			failed++
		}
		fmt.Fprintf(out, "[%-2s] %-16s %s\n", mark, c.Name, c.Detail)
	}
	if failed > 0 {
		fmt.Fprintf(out, "\n%d comprobación(es) pendiente(s)\n", failed)
		return 1
	}
	return 0
}

func cmdClients(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("clients", flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	clients, err := ScanClients()
	if err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	if len(clients) == 0 {
		fmt.Fprintln(out, "no hay dispositivos conectados")
		return 0
	}
	fmt.Fprintf(out, "%-18s %-15s %-17s %5s %9s %9s %10s %8s %7s\n",
		"MAC", "IP", "NOMBRE", "SEÑAL", "BAJADA", "SUBIDA", "CONECTADO", "INTERNET", "PERMITE")
	for _, c := range clients {
		fmt.Fprintf(out, "%-18s %-15s %-17s %5d %9s %9s %10s %8s %7s\n",
			c.MAC, c.IP, c.Hostname, c.Signal,
			humanBytes(c.RxBytes), humanBytes(c.TxBytes),
			c.Connected.Truncate(time.Second), yesno(c.Internet), yesno(c.Allowed))
	}
	return 0
}

func cmdBlock(args []string, out, errw io.Writer) int {
	return deviceToggle("block", "internet cortado a", args, out, errw, func(target string) error {
		return SetInternet(target, false)
	})
}

func cmdUnblock(args []string, out, errw io.Writer) int {
	return deviceToggle("unblock", "internet restaurado a", args, out, errw, func(target string) error {
		return SetInternet(target, true)
	})
}

func cmdDeny(args []string, out, errw io.Writer) int {
	return deviceToggle("deny", "acceso denegado a", args, out, errw, func(target string) error {
		return SetAllowed(target, false)
	})
}

func cmdAllow(args []string, out, errw io.Writer) int {
	return deviceToggle("allow", "acceso permitido a", args, out, errw, func(target string) error {
		return SetAllowed(target, true)
	})
}

func cmdKick(args []string, out, errw io.Writer) int {
	return deviceToggle("kick", "dispositivo desconectado:", args, out, errw, func(target string) error {
		mac, err := resolveMAC(target)
		if err != nil {
			return err
		}
		return hostapdKick(mac)
	})
}

// deviceToggle implements the shared parse/validate/apply flow of the
// single-device commands. It requires exactly one positional target.
func deviceToggle(name, verb string, args []string, out, errw io.Writer, apply func(string) error) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(errw, "uso: hotspot %s <mac|ip>\n", name)
		return 2
	}
	target := fs.Arg(0)
	if err := apply(target); err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "%s %s\n", verb, target)
	return 0
}

func cmdTui(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(errw)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := RunTUI(); err != nil {
		fmt.Fprintln(errw, "error:", err)
		return 1
	}
	return 0
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func defaultChannel(band string) int {
	if band == "5" {
		return 36
	}
	return 6
}

func currentWiFiChannel(iface string) (int, error) {
	raw, err := output("iw", "dev", iface, "info")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "channel ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strconv.Atoi(fields[1])
			}
		}
	}
	return 0, fmt.Errorf("la interfaz %s no está asociada a ningún canal", iface)
}

func randomPassword(n int) string {
	const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			b[i] = chars[0]
			continue
		}
		b[i] = chars[v.Int64()]
	}
	return string(b)
}

func yesno(b bool) string {
	if b {
		return "si"
	}
	return "no"
}
