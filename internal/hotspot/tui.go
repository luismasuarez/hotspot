package hotspot

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tuiRefreshInterval is how often the client list and the state are rescanned.
const tuiRefreshInterval = 2 * time.Second

// tuiMode is the currently visible screen of the panel.
type tuiMode int

const (
	modeClients tuiMode = iota
	modeStart
	modeWifi
	modeRename
	modeConfirmStop
)

// tuiTickMsg drives the periodic refresh.
type tuiTickMsg struct{}

// refreshMsg carries a fresh snapshot of the world back to the model.
type refreshMsg struct {
	clients      []Client
	state        *State
	pol          *Policy
	active       bool
	err          error
	reconcileErr error
}

// status renders the most relevant problem of the snapshot, if any.
func (r refreshMsg) status() string {
	if r.err != nil {
		return "error: " + r.err.Error()
	}
	if r.reconcileErr != nil {
		return "aviso: no se pudo aplicar la política: " + r.reconcileErr.Error()
	}
	return ""
}

// opDoneMsg is returned when an asynchronous up/down operation finishes.
type opDoneMsg struct {
	err error
	log string
}

// tuiColumn is one column of the client table.
type tuiColumn struct {
	width int
	title string
	cell  func(Client) string
}

// tuiModel is the Bubble Tea model of the interactive panel.
type tuiModel struct {
	width, height int

	clients []Client
	cursor  int
	offset  int
	pol     *Policy
	state   *State
	active  bool

	status   string
	help     bool
	mode     tuiMode
	busy     bool
	startLog string

	sp spinner.Model

	// Start / stop panel.
	sourceOptions []Source
	srcIdx        int
	focus         int
	ssid          textinput.Model
	pass          textinput.Model
	open          bool

	// Rename overlay.
	rename      textinput.Model
	renamingMAC string
}

// RunTUI starts the interactive client-management panel.
func RunTUI() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("el panel interactivo requiere privilegios de root (ejecuta con sudo)")
	}
	p := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Styles used across the panel.
var (
	tuiOnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	tuiOffStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tuiCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	tuiTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	tuiHeadStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	tuiDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	tuiStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	tuiErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tuiBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// newTUIModel builds the initial model, scanning the current state once.
func newTUIModel() tuiModel {
	snap := loadSnapshot()
	m := tuiModel{
		sp:      spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(tuiCursorStyle)),
		clients: snap.clients,
		state:   snap.state,
		pol:     snap.pol,
		active:  snap.active,
		status:  snap.status(),
	}
	if !m.active {
		m.enterStart()
	} else {
		m.mode = modeClients
	}
	m.clampCursor()
	return m
}

// loadSnapshot rescans the state, the clients and the policy, reconciling the
// runtime rules as a side effect.
func loadSnapshot() refreshMsg {
	var m refreshMsg
	if err := Reconcile(); err != nil {
		m.reconcileErr = err
	}
	clients, err := ScanClients()
	if err != nil {
		m.err = err
	}
	m.clients = clients
	st, serr := LoadState()
	switch {
	case serr == nil:
		m.state = st
		m.active = true
	case errors.Is(serr, errNoState):
		m.active = false
	default:
		m.err = serr
	}
	if p, perr := LoadPolicy(); perr == nil {
		m.pol = p
	}
	return m
}

func loadSnapshotCmd() tea.Msg { return loadSnapshot() }

func tuiRefreshTick() tea.Cmd {
	return tea.Every(tuiRefreshInterval, func(time.Time) tea.Msg { return tuiTickMsg{} })
}

// Init implements tea.Model.
func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, tuiRefreshTick())
}

// Update implements tea.Model.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeInputs()
		m.clampCursor()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	case tuiTickMsg:
		if m.busy {
			return m, tuiRefreshTick()
		}
		return m, tea.Batch(loadSnapshotCmd, tuiRefreshTick())
	case refreshMsg:
		m.applyRefresh(msg)
		return m, nil
	case opDoneMsg:
		return m.handleOpDone(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *tuiModel) applyRefresh(msg refreshMsg) {
	m.clients = msg.clients
	m.state = msg.state
	m.pol = msg.pol
	m.active = msg.active
	if s := msg.status(); s != "" {
		m.status = s
	}
	if !m.active && m.mode != modeStart {
		m.enterStart()
	}
	m.clampCursor()
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.busy {
		return m, nil
	}
	switch m.mode {
	case modeStart:
		return m.updateStart(msg)
	case modeWifi, modeRename, modeConfirmStop:
		return m.updateOverlay(msg)
	}
	return m.updateClients(k)
}

func (m tuiModel) updateClients(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cursor--
		m.clampCursor()
	case "down", "j":
		m.cursor++
		m.clampCursor()
	case "g":
		m.cursor = 0
		m.clampCursor()
	case "G":
		m.cursor = len(m.clients) - 1
		m.clampCursor()
	case "i":
		m.toggleInternet()
	case "b":
		m.toggleAllowed()
	case "x":
		m.kickClient()
	case "n":
		m.startRename()
	case "r":
		m.status = ""
		return m, loadSnapshotCmd
	case "w":
		m.mode = modeWifi
	case "s":
		if m.active {
			m.mode = modeConfirmStop
		} else {
			m.enterStart()
		}
	case "?":
		m.help = !m.help
	}
	return m, nil
}

func (m tuiModel) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.mode {
	case modeWifi:
		if k == "esc" || k == "w" || k == "q" {
			m.mode = modeClients
		}
	case modeConfirmStop:
		switch k {
		case "s", "y", "enter":
			return m, m.stopCmd()
		case "n", "esc":
			m.mode = modeClients
		}
	case modeRename:
		switch k {
		case "esc":
			m.rename.Blur()
			m.mode = modeClients
		case "enter":
			m.commitRename()
		default:
			var cmd tea.Cmd
			m.rename, cmd = m.rename.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m tuiModel) updateStart(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.focus == 0 {
		m.ssid.Focus()
	} else {
		m.ssid.Blur()
	}
	if m.focus == 1 {
		m.pass.Focus()
	} else {
		m.pass.Blur()
	}
	switch k {
	case "up":
		if len(m.sourceOptions) > 0 {
			m.srcIdx = (m.srcIdx - 1 + len(m.sourceOptions)) % len(m.sourceOptions)
		}
		return m, nil
	case "down":
		if len(m.sourceOptions) > 0 {
			m.srcIdx = (m.srcIdx + 1) % len(m.sourceOptions)
		}
		return m, nil
	case "tab":
		m.focus = (m.focus + 1) % 3
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return m, nil
	case "enter":
		return m, m.beginStart()
	case "esc":
		if m.active {
			m.mode = modeClients
		}
		return m, nil
	}
	if m.focus == 2 {
		if k == " " {
			m.open = !m.open
		}
		return m, nil
	}
	var cmd tea.Cmd
	if m.focus == 0 {
		m.ssid, cmd = m.ssid.Update(msg)
	} else {
		m.pass, cmd = m.pass.Update(msg)
	}
	return m, cmd
}

func (m *tuiModel) enterStart() {
	srcs, err := DetectSources()
	if err != nil {
		m.status = "aviso: " + err.Error()
	}
	m.sourceOptions = tuiSourceOptions(srcs)
	m.srcIdx = 0
	for i, s := range m.sourceOptions {
		if s == SourceVPN {
			m.srcIdx = i
			break
		}
	}
	m.resetInputs()
	m.resizeInputs()
	m.focus = 0
	m.mode = modeStart
}

func tuiSourceOptions(srcs []SourceInfo) []Source {
	seen := map[Source]bool{}
	var out []Source
	for _, s := range srcs {
		if !seen[s.Kind] {
			seen[s.Kind] = true
			out = append(out, s.Kind)
		}
	}
	if len(out) == 0 {
		return []Source{SourceVPN, SourceWiFi, SourceEth}
	}
	return out
}

func (m *tuiModel) resetInputs() {
	m.ssid = textinput.New()
	m.ssid.Prompt = "  "
	m.ssid.Placeholder = defaultSSID
	m.ssid.SetValue(defaultSSID)
	m.ssid.CharLimit = 32

	m.pass = textinput.New()
	m.pass.Prompt = "  "
	m.pass.Placeholder = "vacío = generar una"
	m.pass.CharLimit = 63
	m.pass.EchoMode = textinput.EchoPassword

	m.rename = textinput.New()
	m.rename.Prompt = "  "
	m.rename.CharLimit = 32

	m.open = false
	m.startLog = ""
}

func (m *tuiModel) resizeInputs() {
	w := m.width / 3
	if w < 16 {
		w = 16
	}
	if w > 40 {
		w = 40
	}
	m.ssid.Width = w
	m.pass.Width = w
	m.rename.Width = w
}

// beginStart resolves the configuration and returns the command that runs Up.
func (m *tuiModel) beginStart() tea.Cmd {
	if len(m.sourceOptions) == 0 {
		m.status = "error: no hay orígenes disponibles"
		return nil
	}
	kind := m.sourceOptions[m.srcIdx]
	wifi, err := FirstWireless()
	if err != nil {
		m.status = "error: " + err.Error()
		return nil
	}
	var uplink string
	if kind == SourceWiFi {
		uplink = wifi
	} else if uplink, err = Resolve(kind); err != nil {
		m.status = "error: " + err.Error()
		return nil
	}

	ch := 0
	if kind == SourceWiFi {
		if c, cerr := currentWiFiChannel(wifi); cerr == nil {
			ch = c
		}
	}
	if ch == 0 {
		ch = defaultChannel("2.4")
	}

	apName := wifi
	if kind == SourceWiFi {
		apName = defaultAPName
	}
	dedicated := apName != wifi
	if kind == SourceWiFi && !dedicated {
		m.status = "error: el origen wifi necesita una interfaz AP virtual distinta de la wifi"
		return nil
	}

	ssid := strings.TrimSpace(m.ssid.Value())
	if ssid == "" {
		ssid = defaultSSID
	}
	pass := m.pass.Value()
	if m.open {
		pass = ""
	} else {
		if pass != "" && len(pass) < 8 {
			m.status = "error: la contraseña debe tener al menos 8 caracteres"
			return nil
		}
		if pass == "" {
			pass = randomPassword(12)
		}
	}

	cfg := &Config{
		Source:    kind,
		Uplink:    uplink,
		WiFiIface: wifi,
		SSID:      ssid,
		Pass:      pass,
		Open:      m.open,
		Band:      "2.4",
		Channel:   ch,
		Subnet:    defaultSubnet,
		DNS:       defaultDNS,
		APName:    apName,
		Table:     defaultTable,
		Priority:  defaultPriority,
		MSS:       defaultMSS,
		MTU:       ifaceMTU(uplink),
		Dedicated: dedicated,
		NoQR:      true,
		QRPNG:     "",
	}

	m.busy = true
	m.status = "levantando..."
	m.startLog = ""
	return func() tea.Msg {
		var buf bytes.Buffer
		err := Up(cfg, NewRunner(false, &buf, &buf))
		return opDoneMsg{err: err, log: buf.String()}
	}
}

func (m *tuiModel) stopCmd() tea.Cmd {
	m.busy = true
	m.status = "deteniendo..."
	m.mode = modeClients
	return func() tea.Msg {
		var buf bytes.Buffer
		err := Down(NewRunner(false, &buf, &buf))
		return opDoneMsg{err: err, log: buf.String()}
	}
}

func (m tuiModel) handleOpDone(msg opDoneMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.status = "error: " + msg.err.Error()
		m.startLog = msg.log
		if !m.active {
			m.mode = modeStart
		}
		return m, nil
	}
	m.startLog = ""
	m.applyRefresh(loadSnapshot())
	if m.active {
		m.mode = modeClients
		m.status = "listo"
	} else {
		m.mode = modeStart
		m.status = "detenido"
	}
	return m, nil
}

func (m *tuiModel) toggleInternet() {
	c, ok := m.current()
	if !ok {
		return
	}
	on := !c.Internet
	if err := SetInternet(c.MAC, on); err != nil {
		m.status = "error: " + err.Error()
		return
	}
	if on {
		m.status = "internet activado para " + c.MAC
	} else {
		m.status = "internet cortado para " + c.MAC
	}
	m.rescan()
}

func (m *tuiModel) toggleAllowed() {
	c, ok := m.current()
	if !ok {
		return
	}
	on := !c.Allowed
	if err := SetAllowed(c.MAC, on); err != nil {
		m.status = "error: " + err.Error()
		return
	}
	if on {
		m.status = "acceso permitido a " + c.MAC
	} else {
		m.status = "acceso denegado a " + c.MAC
	}
	m.rescan()
}

func (m *tuiModel) kickClient() {
	c, ok := m.current()
	if !ok {
		return
	}
	if err := hostapdKick(c.MAC); err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.status = "expulsado " + c.MAC
	m.rescan()
}

func (m *tuiModel) startRename() {
	c, ok := m.current()
	if !ok {
		return
	}
	m.renamingMAC = c.MAC
	m.rename.SetValue(m.displayName(c))
	m.rename.CursorEnd()
	m.rename.Focus()
	m.mode = modeRename
}

func (m *tuiModel) commitRename() {
	p, err := LoadPolicy()
	if err != nil {
		m.status = "error: " + err.Error()
		m.rename.Blur()
		m.mode = modeClients
		return
	}
	cp := p.Get(m.renamingMAC)
	cp.Name = sanitizeName(m.rename.Value())
	p.Set(m.renamingMAC, cp)
	if err := p.Save(); err != nil {
		m.status = "error: " + err.Error()
	} else {
		m.status = "dispositivo renombrado"
	}
	m.rename.Blur()
	m.mode = modeClients
	m.rescan()
}

func (m *tuiModel) rescan() {
	snap := loadSnapshot()
	m.clients = snap.clients
	m.state = snap.state
	m.pol = snap.pol
	m.active = snap.active
	if snap.err != nil {
		m.status = "error: " + snap.err.Error()
	}
	m.clampCursor()
}

func (m tuiModel) current() (Client, bool) {
	if m.cursor < 0 || m.cursor >= len(m.clients) {
		return Client{}, false
	}
	return m.clients[m.cursor], true
}

func (m *tuiModel) clampCursor() {
	if len(m.clients) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.clients) {
		m.cursor = len(m.clients) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	vis := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m tuiModel) visibleRows() int {
	v := m.height - 12
	if v < 1 {
		return 8
	}
	return v
}

// displayName resolves the friendly name of a client, preferring the persisted
// policy name, then the hostname and finally the MAC.
func (m tuiModel) displayName(c Client) string {
	if m.pol != nil {
		if name := sanitizeName(m.pol.Get(c.MAC).Name); name != "" {
			return name
		}
	}
	if c.Hostname != "" {
		return sanitizeName(c.Hostname)
	}
	return c.MAC
}

// View implements tea.Model.
func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n\n")

	switch m.mode {
	case modeStart:
		b.WriteString(m.startView())
	case modeWifi:
		b.WriteString(m.wifiView())
	case modeRename:
		b.WriteString(m.clientsView())
		b.WriteString("\n")
		b.WriteString(m.renameView())
	case modeConfirmStop:
		b.WriteString(m.clientsView())
		b.WriteString("\n\n")
		b.WriteString(m.confirmView())
	default:
		b.WriteString(m.clientsView())
	}

	b.WriteString("\n")
	switch {
	case m.busy:
		b.WriteString(m.sp.View() + " " + m.status)
	case m.status != "":
		b.WriteString(m.statusStyleFor().Render(m.status))
	}
	b.WriteString("\n")
	if m.help {
		b.WriteString(m.helpView())
	} else {
		b.WriteString(m.footerView())
	}
	return b.String()
}

func (m tuiModel) statusStyleFor() lipgloss.Style {
	if strings.HasPrefix(m.status, "error") || strings.HasPrefix(m.status, "aviso") {
		return tuiErrStyle
	}
	return tuiStatusStyle
}

func (m tuiModel) headerView() string {
	status := "inactivo"
	statusStyle := tuiOffStyle
	if m.active {
		status = "activo"
		statusStyle = tuiOnStyle
	}
	var lines []string
	lines = append(lines, tuiTitleStyle.Render("hotspot")+" · "+statusStyle.Render(status))
	if m.active && m.state != nil {
		ssid := m.state.SSID
		if ssid == "" {
			ssid = "—"
		}
		lines = append(lines, fmt.Sprintf("ssid: %s   origen: %s   uplink: %s   clientes: %d",
			ssid, m.state.Source, m.state.Uplink, len(m.clients)))
		lines = append(lines, fmt.Sprintf("inicio: %s   uptime: %s",
			m.state.StartedAt.Format("15:04:05"), humanDur(time.Since(m.state.StartedAt))))
	} else {
		lines = append(lines, tuiDimStyle.Render("sin hotspot en marcha"))
	}
	body := strings.Join(lines, "\n")
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	return tuiBoxStyle.Width(w).Render(body)
}

func (m tuiModel) columns() []tuiColumn {
	w := m.width
	if w <= 0 {
		w = 100
	}
	cols := []tuiColumn{
		{18, "NOMBRE", func(c Client) string { return m.displayName(c) }},
		{15, "IP", func(c Client) string { return c.IP }},
		{17, "MAC", func(c Client) string { return c.MAC }},
	}
	optional := []tuiColumn{
		{6, "SEÑAL", func(c Client) string { return signalText(c.Signal) }},
		{9, "BAJADA", func(c Client) string { return humanBytes(c.RxBytes) }},
		{9, "SUBIDA", func(c Client) string { return humanBytes(c.TxBytes) }},
		{10, "CONECT.", func(c Client) string { return humanDur(c.Connected) }},
	}
	tail := []tuiColumn{
		{9, "INTERNET", tuiBoolCell(func(c Client) bool { return c.Internet })},
		{9, "PERMITE", tuiBoolCell(func(c Client) bool { return c.Allowed })},
	}

	used := len(cols) - 1
	for _, c := range cols {
		used += c.width
	}
	for _, oc := range optional {
		if used+oc.width+1 <= w {
			cols = append(cols, oc)
			used += oc.width + 1
		}
	}
	cols = append(cols, tail...)

	total := len(cols) - 1
	for _, c := range cols {
		total += c.width
	}
	if total > w {
		over := total - w
		if cols[0].width-over >= 8 {
			cols[0].width -= over
		} else {
			cols[0].width = 8
		}
	}
	return cols
}

func (m tuiModel) clientsView() string {
	if len(m.clients) == 0 {
		return tuiDimStyle.Render("no hay dispositivos conectados")
	}
	cols := m.columns()
	var b strings.Builder
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = tuiPad(tuiClip(c.title, c.width), c.width)
	}
	b.WriteString(tuiHeadStyle.Render(strings.Join(heads, " ")))
	b.WriteString("\n")

	vis := m.visibleRows()
	end := m.offset + vis
	if end > len(m.clients) {
		end = len(m.clients)
	}
	for i := m.offset; i < end; i++ {
		c := m.clients[i]
		cells := make([]string, len(cols))
		for j, col := range cols {
			cells[j] = tuiPad(tuiClip(col.cell(c), col.width), col.width)
		}
		row := strings.Join(cells, " ")
		if i == m.cursor {
			b.WriteString(tuiCursorStyle.Render("> ") + row)
		} else {
			b.WriteString("  " + row)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) startView() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("arrancar hotspot"))
	b.WriteString("\n\n")
	b.WriteString(tuiHeadStyle.Render("ORIGEN"))
	b.WriteString("\n")
	for i, s := range m.sourceOptions {
		if i == m.srcIdx {
			b.WriteString(tuiCursorStyle.Render("> " + string(s)))
		} else {
			b.WriteString("  " + string(s))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(tuiHeadStyle.Render("SSID"))
	b.WriteString("\n")
	b.WriteString(m.ssid.View())
	b.WriteString("\n\n")
	b.WriteString(tuiHeadStyle.Render("CONTRASEÑA"))
	b.WriteString("\n")
	b.WriteString(m.pass.View())
	b.WriteString("\n\n")
	check := "[ ]"
	if m.open {
		check = tuiOnStyle.Render("[x]")
	}
	b.WriteString(check + " red abierta (sin contraseña)")
	if m.startLog != "" {
		b.WriteString("\n\n")
		b.WriteString(tuiErrStyle.Render("salida del comando:"))
		b.WriteString("\n")
		b.WriteString(tuiLogView(m.startLog))
	}
	return b.String()
}

func (m tuiModel) wifiView() string {
	if !m.active || m.state == nil {
		return tuiDimStyle.Render("no hay hotspot activo")
	}
	st := m.state
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("información wifi"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "ssid:   %s\n", st.SSID)
	if st.Open {
		b.WriteString("pass:   (abierta, sin contraseña)\n")
	} else if st.Pass == "" {
		b.WriteString("pass:   (no guardada)\n")
	} else {
		fmt.Fprintf(&b, "pass:   %s\n", st.Pass)
	}
	fmt.Fprintf(&b, "origen: %s   uplink: %s\n\n", st.Source, st.Uplink)

	var buf bytes.Buffer
	if err := renderQR(&buf, wifiPayload(st.SSID, st.Pass, st.Open, st.Hidden), false); err != nil {
		b.WriteString(tuiErrStyle.Render("no se pudo generar el QR: " + err.Error()))
	} else {
		b.WriteString(buf.String())
	}
	return b.String()
}

func (m tuiModel) renameView() string {
	return tuiTitleStyle.Render("renombrar "+m.renamingMAC) + "\n" + m.rename.View()
}

func (m tuiModel) confirmView() string {
	return tuiErrStyle.Render("¿detener el hotspot?") + "  " + tuiDimStyle.Render("s = sí · n/esc = no")
}

func (m tuiModel) footerView() string {
	if m.mode == modeStart {
		return tuiDimStyle.Render("↑/↓ origen · tab campo · espacio abierta · enter arrancar · esc volver · q salir")
	}
	return tuiDimStyle.Render("↑/↓ mover · i internet · b permitir · x expulsar · n renombrar · r refrescar · w wifi · s arrancar/parar · ? ayuda · q salir")
}

func (m tuiModel) helpView() string {
	return strings.Join([]string{
		tuiHeadStyle.Render("AYUDA"),
		"  ↑/↓, j/k   mover el cursor",
		"  i          activar o cortar internet",
		"  b          permitir o denegar la conexión",
		"  x          expulsar el dispositivo ahora",
		"  n          renombrar el dispositivo",
		"  r          refrescar la lista",
		"  w          ver ssid, contraseña y QR",
		"  s          arrancar o detener el hotspot",
		"  ?          ocultar esta ayuda",
		"  q          salir",
	}, "\n")
}

func tuiBoolCell(get func(Client) bool) func(Client) string {
	return func(c Client) string {
		if get(c) {
			return tuiOnStyle.Render("SÍ")
		}
		return tuiOffStyle.Render("NO")
	}
}

// tuiClip truncates a string to width using lipgloss (rune/cell aware).
func tuiClip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// tuiPad right-pads s to width, measuring the visible width.
func tuiPad(s string, width int) string {
	if d := width - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func tuiLogView(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	for i, l := range lines {
		lines[i] = tuiClip(l, 100)
	}
	return strings.Join(lines, "\n")
}

func signalText(dbm int) string {
	if dbm == 0 {
		return "n/d"
	}
	return fmt.Sprintf("%d", dbm)
}

func humanDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
