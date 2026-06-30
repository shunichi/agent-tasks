package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// cmdTUI は一覧+詳細をインタラクティブに閲覧する常駐ビューワー (Bubble Tea)。
// 既存の一回出力 CLI (list/show) とは別物で、別セッションが裏で更新するストアを
// 一定間隔で再読込し、変化があれば再描画する (1 マシン前提の軽量ポーリング)。
//
// list と同じスコープ規則 (既定は現在 project、--all-projects で横断、--project 明示) を踏み、
// store.go の loadTasks / parseTask と各種サマリヘルパ (prSummary/timestampSummary 等) を再利用する。
func cmdTUI(args []string) error {
	var filterStatus, filterProject string
	allProjects := false
	showDone := false
	interval := 2 * time.Second

	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--status":
			v, err := s.value("--status")
			if err != nil {
				return err
			}
			filterStatus = v
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProject = v
		case "--all-projects":
			allProjects = true
		case "--all", "-a":
			showDone = true
		case "--interval":
			v, err := s.value("--interval")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return usagef("--interval must be a positive integer (秒): %q", v)
			}
			interval = time.Duration(n) * time.Second
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	// TUI は端末を占有するので、TTY でなければ案内して終わる (watch と同じ思想)。
	if !isTTY(os.Stdout) {
		return fmt.Errorf("tui は端末でのみ使えます (パイプ/リダイレクト不可)。一覧は `agent-tasks` を使ってください")
	}

	current := currentProject()
	effProject, _ := resolveListScope(filterProject, allProjects, current)

	m := &tuiModel{
		dir:           storeDir(),
		effProject:    effProject,
		current:       current,
		projectPinned: filterProject != "", // --project 明示時は p トグルで横断に切り替えない
		filterStatus:  filterStatus,
		showDone:      showDone,
		interval:      interval,
	}
	m.reload() // 最初のフレームからデータを出すため起動前に1回読む

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// tuiModel は TUI の状態。Bubble Tea の Model。
type tuiModel struct {
	dir           string
	effProject    string // 実効 project スコープ ("" = 横断)
	current       string // 現在 project (p トグルの戻り先)
	projectPinned bool   // --project 明示。p トグルを無効化する
	filterStatus  string // "" = 全 status
	showDone      bool
	interval      time.Duration

	all  []Task // ストア全タスク (project/id 昇順)
	rows []Task // フィルタ適用後の表示行

	cursor  int // rows のインデックス
	top     int // 一覧のスクロール先頭 (表示窓の先頭行)
	listH   int // 一覧ペインの表示行数 (レイアウトで更新)
	leftW   int // 一覧ペインの桁幅 (レイアウトで更新)
	vp      viewport.Model
	ready   bool
	width   int
	height  int
	sig     uint64    // ストアの変更検知シグネチャ
	updated time.Time // 最終再読込時刻
	loadErr error
}

type tuiTickMsg time.Time

func tuiTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tuiTickMsg(t) })
}

func (m *tuiModel) Init() tea.Cmd {
	return tuiTick(m.interval)
}

// taskKey は選択を再読込後も保つための同定キー (project/id)。
func taskKey(t Task) string { return t.Project + "/" + t.ID }

// reload はストアを読み直し、変更検知シグネチャを更新してフィルタを再適用する。
// loadTasks 失敗時は loadErr に残し、前回のタスクは保持する (一時的な読み取り失敗で
// 画面を空にしない)。
func (m *tuiModel) reload() {
	tasks, err := loadTasks(m.dir)
	m.loadErr = err
	if err == nil {
		m.all = tasks
	}
	m.sig = storeSignature(m.dir)
	m.updated = time.Now()
	m.applyFilter()
}

// applyFilter は all から表示行を絞り込む (list の selectTasks と同じ規則)。
// 選択は project/id で再特定して保持し、消えていれば範囲内にクランプする。
func (m *tuiModel) applyFilter() {
	prevKey := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		prevKey = taskKey(m.rows[m.cursor])
	}
	hideDone := !m.showDone && m.filterStatus == ""
	rows := m.rows[:0:0]
	for _, t := range m.all {
		if m.effProject != "" && t.Project != m.effProject {
			continue
		}
		if m.filterStatus != "" && t.Status != m.filterStatus {
			continue
		}
		if hideDone && t.Status == "done" {
			continue
		}
		rows = append(rows, t)
	}
	m.rows = rows

	m.cursor = 0
	if prevKey != "" {
		for i, t := range rows {
			if taskKey(t) == prevKey {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
	m.syncDetail()
}

func (m *tuiModel) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.fixScroll()
}

// fixScroll は cursor が一覧の表示窓 [top, top+listH) に収まるよう top を調整する。
func (m *tuiModel) fixScroll() {
	if m.listH <= 0 {
		return
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+m.listH {
		m.top = m.cursor - m.listH + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// syncDetail は選択行の詳細を viewport に流し込み、先頭へ戻す。
func (m *tuiModel) syncDetail() {
	if !m.ready {
		return
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		m.vp.SetContent(tuiNoSelection)
		m.vp.GotoTop()
		return
	}
	m.vp.SetContent(tuiDetail(m.rows[m.cursor]))
	m.vp.GotoTop()
}

const tuiNoSelection = "(タスクなし)"

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.syncDetail()
		return m, nil

	case tuiTickMsg:
		if sig := storeSignature(m.dir); sig != m.sig {
			m.reload()
		}
		return m, tuiTick(m.interval)

	case tea.MouseMsg:
		// マウスホイールは詳細ペインのスクロールに使う。
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			m.cursor--
			m.clampCursor()
			m.syncDetail()
		case "down", "j":
			m.cursor++
			m.clampCursor()
			m.syncDetail()
		case "home", "g":
			m.cursor = 0
			m.clampCursor()
			m.syncDetail()
		case "end", "G":
			m.cursor = len(m.rows) - 1
			m.clampCursor()
			m.syncDetail()
		case "pgup", "ctrl+u", "K":
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(tea.KeyMsg{Type: tea.KeyPgUp})
			return m, cmd
		case "pgdown", "ctrl+d", "J":
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(tea.KeyMsg{Type: tea.KeyPgDown})
			return m, cmd
		case "a":
			m.showDone = !m.showDone
			m.applyFilter()
		case "s":
			m.filterStatus = cycleStatus(m.filterStatus)
			m.applyFilter()
		case "p":
			if !m.projectPinned && m.current != "" {
				if m.effProject == "" {
					m.effProject = m.current
				} else {
					m.effProject = ""
				}
				m.applyFilter()
			}
		case "r":
			m.reload()
		}
		return m, nil
	}
	return m, nil
}

// cycleStatus は status フィルタを循環させる (全 → todo → in-progress → blocked → review → done → 全)。
func cycleStatus(cur string) string {
	order := []string{"", "todo", "in-progress", "blocked", "review", "done"}
	for i, s := range order {
		if s == cur {
			return order[(i+1)%len(order)]
		}
	}
	return ""
}

func (m *tuiModel) View() string {
	if !m.ready {
		return "起動中…"
	}
	left := m.renderList()
	sep := tuiSepStyle.Height(m.listH).Render(strings.Repeat("│\n", m.listH))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, m.vp.View())
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
}

// layout はウィンドウサイズから各ペインの寸法を決める。header/footer に各1行を使い、
// 残りを一覧 (左) と詳細 (右) で分ける。
func (m *tuiModel) layout() {
	contentH := m.height - 2 // header + footer
	if contentH < 1 {
		contentH = 1
	}
	m.listH = contentH

	leftW := m.width * 2 / 5
	leftW = clampInt(leftW, 24, 64)
	if leftW > m.width-12 {
		leftW = m.width - 12
	}
	if leftW < 1 {
		leftW = 1
	}
	m.leftW = leftW

	rightW := m.width - leftW - 3 // セパレータ "│" + 前後の余白
	if rightW < 1 {
		rightW = 1
	}
	m.vp.Width = rightW
	m.vp.Height = contentH
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// 列幅・スタイル定義
var (
	tuiHeaderStyle = lipgloss.NewStyle().Bold(true)
	tuiFooterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiSepStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiPointStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	tuiBoldStyle   = lipgloss.NewStyle().Bold(true)
)

// statusStyle は status 名の色を ANSI パレット (render.go の palette) に合わせる。
func statusStyle(status string) lipgloss.Style {
	switch status {
	case "in-progress":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	case "blocked":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	case "review":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")) // magenta
	case "done":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // todo: white
	}
}

func (m *tuiModel) renderHeader() string {
	scope := m.effProject
	if scope == "" {
		scope = "全 project"
	}
	filt := "all"
	if m.filterStatus != "" {
		filt = m.filterStatus
	}
	done := "隠す"
	if m.showDone {
		done = "表示"
	}
	left := tuiHeaderStyle.Render("agent-tasks tui")
	info := tuiDimStyle.Render(fmt.Sprintf("  %s  status:%s  done:%s  %d件  %s",
		scope, filt, done, len(m.rows), m.updated.Format("15:04:05")))
	line := left + info
	if m.loadErr != nil {
		line += statusStyle("blocked").Render("  読み取りエラー")
	}
	return tuiTrunc(line, m.width)
}

func (m *tuiModel) renderFooter() string {
	keys := "↑↓/jk 選択  PgUp/PgDn 詳細  a done  s status  p project  r 更新  q 終了"
	return tuiFooterStyle.Render(tuiTrunc(keys, m.width))
}

// renderList は一覧ペイン (左) を表示窓ぶんだけ描く。cursor 行はポインタ + 太字で示す。
func (m *tuiModel) renderList() string {
	w := m.leftW
	if len(m.rows) == 0 {
		msg := "該当タスクなし"
		if m.loadErr != nil {
			msg = "ストアを読めません"
		}
		return lipgloss.NewStyle().Width(w).Height(m.listH).Render(tuiDimStyle.Render(msg))
	}

	cross := m.effProject == ""
	projW := 0
	if cross {
		for _, t := range m.rows {
			if dw := dispWidth(t.Project); dw > projW {
				projW = dw
			}
		}
		projW = clampInt(projW, 0, 16)
	}
	// 行構成: "❯ " + [project] + id(4) + " " + status(11) + " " + title
	const idW, stW = 4, 11
	fixed := 2 + idW + 1 + stW + 1
	if cross {
		fixed += projW + 1
	}
	titleW := w - fixed
	if titleW < 4 {
		titleW = 4
	}

	end := m.top + m.listH
	if end > len(m.rows) {
		end = len(m.rows)
	}
	var b strings.Builder
	for i := m.top; i < end; i++ {
		t := m.rows[i]
		selected := i == m.cursor
		ptr := "  "
		if selected {
			ptr = tuiPointStyle.Render("❯ ")
		}
		var line strings.Builder
		line.WriteString(ptr)
		if cross {
			line.WriteString(tuiDimStyle.Render(padDisp(t.Project, projW)))
			line.WriteByte(' ')
		}
		line.WriteString(tuiDimStyle.Render(fmt.Sprintf("%-*s", idW, t.ID)))
		line.WriteByte(' ')
		line.WriteString(statusStyle(t.Status).Render(fmt.Sprintf("%-*s", stW, t.Status)))
		line.WriteByte(' ')

		title := tuiListTitle(t)
		title = truncateDisp(title, titleW)
		if selected {
			title = tuiBoldStyle.Render(title)
		}
		line.WriteString(title)
		b.WriteString(line.String())
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return lipgloss.NewStyle().Width(w).Height(m.listH).MaxHeight(m.listH).Render(b.String())
}

// tuiListTitle は一覧に出すタイトル。in-progress でセッション状態が分かれば
// [waiting]/[working]/[ended] を先頭に添える (どの pane が応答待ちかを一覧で掴むため)。
func tuiListTitle(t Task) string {
	title := blockedTitle(t) // blocked なら理由が添う既存整形を流用
	if t.Status != "in-progress" {
		return title
	}
	st, ok := taskSessionState(t)
	if !ok {
		return title
	}
	switch st.State {
	case sessWaiting:
		return "[waiting] " + title
	case sessWorking:
		return "[working] " + title
	case sessEnded:
		return "[ended] " + title
	}
	return title
}

// padDisp は表示幅 w に右詰めパディングする (CJK 幅対応)。w を超える場合は truncate。
func padDisp(s string, w int) string {
	s = truncateDisp(s, w)
	if pad := w - dispWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func tuiTrunc(s string, w int) string {
	if w <= 0 {
		return s
	}
	return truncateDisp(s, w)
}

// tuiDetail は詳細ペインの内容を組み立てる。ファイル全文 (frontmatter + 本文) に、
// show と同じ PR 一覧 / 所要時間サマリを末尾へ添える。色は付けず素のテキストにする
// (viewport の折返しと相性が良く、TUI 側の lipgloss スタイルと混ざらない)。
func tuiDetail(t Task) string {
	data, err := os.ReadFile(t.Path)
	body := string(data)
	if err != nil {
		body = fmt.Sprintf("(読み込み失敗: %v)", err)
	}
	now := time.Now()
	var footers []string
	if s := prSummary(t, colors{}); s != "" {
		footers = append(footers, s)
	}
	if s := timestampSummary(t, now, colors{}); s != "" {
		footers = append(footers, s)
	}
	out := strings.TrimRight(body, "\n")
	if len(footers) > 0 {
		out += "\n\n" + strings.Join(footers, "\n")
	}
	return out + "\n"
}

// storeSignature はストア配下の *.md の (相対パス, mtime, size) を畳み込んだ
// 変更検知シグネチャを返す。tick ごとにこれを比べ、変わったときだけ再読込する
// (毎回の全 parse を避ける軽量ポーリング)。読めないときは 0。
func storeSignature(dir string) uint64 {
	h := fnv.New64a()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(dir, e.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			fmt.Fprintf(h, "%s/%s:%d:%d;", e.Name(), f.Name(), info.ModTime().UnixNano(), info.Size())
		}
	}
	return h.Sum64()
}
