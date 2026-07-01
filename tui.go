package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
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

	cursor     int    // rows のインデックス
	top        int    // 一覧のスクロール先頭 (表示窓の先頭行)
	listH      int    // 一覧ペインの表示行数 (レイアウトで更新)
	leftW      int    // 一覧ペインの桁幅 (レイアウトで更新)
	showDetail bool   // 詳細ペインを表示中か (起動直後は false = リストのみ。Enter で表示)
	showHelp   bool   // ヘルプ (キーバインド一覧) を表示中か (? で開閉)
	flash      string // 一時メッセージ (コピー結果など)。次のキー入力で消える
	vertical   bool   // 詳細を下に積む縦分割か (狭い/縦長端末。広いと横分割で右に出す)
	vp         viewport.Model
	ready      bool
	width      int
	height     int
	sig        uint64    // ストアの変更検知シグネチャ
	updated    time.Time // 最終再読込時刻
	loadErr    error
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
		if t.Incomplete {
			continue // 作成途中 (title 未記入) の予約は表示しない
		}
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
		m.vp.SetContent(m.wrapDetail(tuiNoSelection))
		m.vp.GotoTop()
		return
	}
	m.vp.SetContent(m.wrapDetail(tuiDetail(m.rows[m.cursor])))
	m.vp.GotoTop()
}

// wrapDetail は詳細本文を viewport 幅で折り返す。bubbles の viewport は長い行を
// 折り返さず、はみ出した分を横方向に切り捨てて表示するため、ここで明示的に折り返して
// 「行が切れて読めない」のを防ぐ (横分割・縦分割どちらでも効く)。
func (m *tuiModel) wrapDetail(s string) string {
	w := m.vp.Width
	if w < 1 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
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

	case copyResultMsg:
		if msg.err != nil {
			m.flash = "コピー失敗: " + msg.err.Error()
		} else {
			m.flash = "コピーしました: " + msg.text
		}
		return m, nil

	case tea.MouseMsg:
		// マウスホイールは詳細ペインのスクロールに使う。
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		m.flash = "" // 一時メッセージは次のキー入力で消す
		// ヘルプ表示中は ?/q/Esc で閉じるだけ (他キーは無効化)。Ctrl+C は常に終了。
		if m.showHelp {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "?", "q", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "q", "esc":
			// 詳細表示中なら詳細を閉じてリストへ戻る。リストのみなら終了する。
			if m.showDetail {
				m.showDetail = false
				m.layout()
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			// 選択中タスクの詳細を右に表示する。
			if len(m.rows) > 0 {
				m.showDetail = true
				m.layout()
				m.syncDetail()
			}
			return m, nil
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
		case "c":
			// 選択タスクの `start task <NNNN>` をクリップボードへコピーする (任意の pane の
			// claude に貼って着手できる)。着手の意味がある todo / blocked のみ対象。
			// コピーは外部コマンドの起動を伴い得るので tea.Cmd で非同期実行し、実際の
			// 成否を copyResultMsg で受けてフラッシュ表示する (UI をブロックしない)。
			if t, ok := m.selectedTask(); ok {
				if cmdStr, startable := startCommandFor(t); startable {
					return m, copyCmd(cmdStr)
				}
				m.flash = "コピー対象は todo / blocked のタスクのみです"
			}
			return m, nil
		case "r":
			m.reload()
		}
		return m, nil
	}
	return m, nil
}

// selectedTask は選択中の行のタスクを返す。行が無ければ ok=false。
func (m *tuiModel) selectedTask() (Task, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Task{}, false
	}
	return m.rows[m.cursor], true
}

// startCommandFor は TUI からコピーする start コマンド文字列 ("start task <N>") を返す。
// 着手の意味があるのは todo / blocked のタスクのみ (それ以外は ok=false)。
func startCommandFor(t Task) (string, bool) {
	switch t.Status {
	case "todo", "blocked":
		return "start task " + t.ID, true
	}
	return "", false
}

// copyResultMsg は非同期コピーの結果 (成否と対象文字列)。Update がフラッシュ表示に使う。
type copyResultMsg struct {
	text string
	err  error
}

// copyCmd は s のクリップボードコピーを非同期で行い、結果を copyResultMsg で返す tea.Cmd。
// 外部コマンド起動を伴い得るので UI スレッドをブロックしない。
func copyCmd(s string) tea.Cmd {
	return func() tea.Msg {
		return copyResultMsg{text: s, err: copyToClipboard(s)}
	}
}

// copyToClipboard はテキストをクリップボードへコピーする。まず OS のクリップボードコマンドで
// システムクリップボードへ直接書き (確実)、見つからなければ OSC52 エスケープを端末へ書く
// フォールバックを使う (外部ツールの無い SSH 先など。ただし tmux/端末が OSC52 を許可して
// いないと届かないことがある)。
func copyToClipboard(s string) error {
	if name, args, ok := clipboardTool(); ok {
		if err := runClipboard(name, args, s); err == nil {
			return nil
		}
		// ツールはあるが失敗 (DISPLAY 無し等) → OSC52 にフォールバック。
	}
	_, err := osc52.New(s).WriteTo(os.Stderr)
	return err
}

// clipboardTool は使えるクリップボード書き込みコマンドを返す (PATH にある最初のもの)。
func clipboardTool() (name string, args []string, ok bool) {
	for _, c := range []struct {
		name string
		args []string
	}{
		{"wl-copy", nil}, // Wayland
		{"xclip", []string{"-selection", "clipboard"}}, // X11
		{"xsel", []string{"--clipboard", "--input"}},   // X11 (自分で背景化)
		{"pbcopy", nil},   // macOS
		{"clip.exe", nil}, // WSL/Windows
	} {
		if _, err := exec.LookPath(c.name); err == nil {
			return c.name, c.args, true
		}
	}
	return "", nil, false
}

// runClipboard はクリップボードコマンドに s を流し込む。xclip のように選択を供給し続けて
// 終了しない (前景型) ツールでも UI をブロックしないよう、短時間だけ完了を待ち、まだ
// 動いていれば成功とみなしてバックグラウンドで後始末する (プロセスは選択を供給中)。
func runClipboard(name string, args []string, s string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }() // 速やかに終了するツールの結果取得 + 後始末
	select {
	case err := <-done:
		return err
	case <-time.After(200 * time.Millisecond):
		return nil // まだ選択を供給中 (xclip 前景型) → 成功扱い。goroutine が後で reap する
	}
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
	if m.showHelp {
		return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.renderHelp(), m.renderFooter())
	}
	var body string
	switch {
	case !m.showDetail:
		body = m.renderList() // リストのみ (全面)
	case m.vertical:
		// 縦分割: 一覧 (上) / 区切り線 / 詳細 (下)。tig の縦長レイアウトと同じ向き。
		sep := tuiSepStyle.Render(strings.Repeat("─", max1(m.width)))
		body = lipgloss.JoinVertical(lipgloss.Left, m.renderList(), sep, m.vp.View())
	default:
		// 横分割: 一覧 (左) / 区切り線 / 詳細 (右)。
		sep := tuiSepStyle.Height(m.listH).Render(strings.Repeat("│\n", m.listH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(), sep, m.vp.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
}

// layout はウィンドウサイズから各ペインの寸法を決める。header/footer に各1行を使い、
// 残りを一覧 (左) と詳細 (右) で分ける。
func (m *tuiModel) layout() {
	contentH := m.height - 2 // header + footer
	if contentH < 1 {
		contentH = 1
	}

	// 詳細を出していないときは一覧がウィンドウ全面を使う。
	if !m.showDetail {
		m.leftW = max1(m.width)
		m.listH = contentH
		m.fixScroll()
		return
	}

	// 分割の向き: 一覧をタイトルが収まる「自然幅」にしたとき詳細ペインに残る実効幅で決める。
	// 残り幅が読み幅 (tuiMinDetailWidth) に満たないなら、一覧を切り詰めてまで横に並べず、
	// 縦分割 (詳細を下、tig 風) にしてウィンドウ全幅を詳細に与える (横方向の切り詰め回避)。
	// ウィンドウ幅だけでなく一覧の自然幅も加味するので、「幅はあるが一覧が広くて詳細が
	// 狭い」場合も下積みになる。
	leftW, detailW, vertical := detailLayout(m.width, m.listNaturalWidth())
	m.vertical = vertical

	if m.vertical {
		// 縦分割: 一覧を上、詳細を下に積む。間に区切り線 1 行。
		m.leftW = max1(m.width)
		listH := clampInt(contentH*2/5, 3, contentH-4)
		if listH < 1 {
			listH = 1
		}
		m.listH = listH
		m.vp.Width = max1(m.width)
		m.vp.Height = max1(contentH - listH - 1)
		m.fixScroll()
		return
	}

	// 横分割: 一覧 (左, 自然幅) / 区切り / 詳細 (右, 残り)。
	m.listH = contentH
	m.leftW = leftW
	m.vp.Width = max1(detailW)
	m.vp.Height = contentH
	m.fixScroll()
}

// detailLayout は横分割を仮定したときの一覧幅 (左) と詳細ペインの実効幅、そして縦分割に
// 倒すべきか (詳細がウィンドウ全幅を必要とするほど狭いか) を返す。layout から切り出して
// 単体テストしやすくしている。一覧は自然幅 (タイトルが収まる幅) を基準にし、最小幅
// tuiMinListWidth とウィンドウ幅で挟む。
func detailLayout(width, natural int) (listW, detailW int, vertical bool) {
	listW = clampInt(natural, tuiMinListWidth, max1(width))
	detailW = width - listW - 3 // セパレータ "│" + 前後の余白
	vertical = detailW < tuiMinDetailWidth
	return
}

const (
	// tuiMinDetailWidth は詳細ペインを読むのに最低限欲しい表示幅。横分割で一覧を自然幅に
	// したとき詳細がこれを下回るなら縦分割 (詳細を下) にする。
	tuiMinDetailWidth = 50
	// tuiMinListWidth は横分割で一覧に確保する最小幅。
	tuiMinListWidth = 24
)

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
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
	if m.flash != "" {
		line += "  " + tuiPointStyle.Render(m.flash)
	}
	return tuiTrunc(line, m.width)
}

func (m *tuiModel) renderFooter() string {
	var keys string
	switch {
	case m.showHelp:
		keys = "?/q/Esc 閉じる"
	case m.showDetail:
		keys = "↑↓/jk 選択  PgUp/PgDn スクロール  c コピー  a done  s status  p project  r 更新  ? ヘルプ  q/Esc 詳細を閉じる"
	default:
		keys = "↑↓/jk 選択  Enter 詳細  c コピー  a done  s status  p project  r 更新  ? ヘルプ  q/Esc 終了"
	}
	return tuiFooterStyle.Render(tuiTrunc(keys, m.width))
}

// helpEntries は表示する (キー, 説明) の一覧。renderHelp とテストで共有する。
func helpEntries() [][2]string {
	return [][2]string{
		{"↑/↓, j/k", "選択を上下に移動"},
		{"g / G", "先頭 / 末尾へ"},
		{"Enter", "選択タスクの詳細を開く"},
		{"PgUp/PgDn, K/J", "詳細をスクロール"},
		{"Ctrl+U / Ctrl+D", "詳細を半画面スクロール"},
		{"マウスホイール", "詳細をスクロール"},
		{"c", "選択タスクの start task <NNNN> をクリップボードへコピー"},
		{"a", "done タスクの表示 / 非表示を切替"},
		{"s", "status フィルタを循環 (全→todo→…→done)"},
		{"p", "現在 project のみ / 全 project を切替"},
		{"r", "ストアを今すぐ再読込 (通常は自動で更新)"},
		{"?", "このヘルプを開閉"},
		{"q / Esc", "詳細を閉じる / (一覧で) 終了"},
		{"Ctrl+C", "終了"},
	}
}

// renderHelp はキーバインド一覧を枠付きパネルで content 領域 (header/footer を除いた高さ) に
// 描く。枠の幅は端末幅に収め (はみ出さない)、説明は必要なら折り返す。極端に狭い/低い端末では
// MaxHeight/折り返しで破綻せず収める。
func (m *tuiModel) renderHelp() string {
	contentH := max1(m.height - 2) // header + footer

	entries := helpEntries()
	keyW := 0
	for _, e := range entries {
		if d := dispWidth(e[0]); d > keyW {
			keyW = d
		}
	}

	var b strings.Builder
	b.WriteString(tuiBoldStyle.Render("キーバインド"))
	b.WriteString("\n\n")
	for _, e := range entries {
		pad := strings.Repeat(" ", keyW-dispWidth(e[0]))
		b.WriteString(tuiBoldStyle.Render(e[0]))
		b.WriteString(pad + "  " + e[1] + "\n")
	}
	b.WriteString("\n")
	b.WriteString(tuiDimStyle.Render("ストアの変更は自動で反映されます (r で即時)。"))

	// 枠の内側幅 = 端末幅から枠 (2) + 左右パディング (2) を引いた幅。広すぎても読みやすい幅に留める。
	innerW := m.width - 4
	if innerW > 72 {
		innerW = 72
	}
	if innerW < 1 {
		innerW = 1
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(innerW).
		Render(b.String())

	// content 領域ちょうどに収める (footer を最下部に固定。高すぎる枠は下端を切る)。
	return lipgloss.NewStyle().Width(max1(m.width)).Height(contentH).MaxHeight(contentH).Render(box)
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

	cross, projW, sessW, fixed := m.listCols()
	// 右端に UPDATED 列を出す (幅が足りなければ諦めて title を優先)。
	rightCol := 0
	if updW := m.updatedColWidth(); updW > 0 {
		rightCol = updW + 2 // title との最小余白
	}
	titleW := w - fixed - rightCol
	if titleW < 4 {
		titleW = w - fixed
		rightCol = 0
	}
	if titleW < 4 {
		titleW = 4
	}

	// UPDATED の桁位置をそろえる: タイトル列幅 = 表示タイトルの最大幅 (titleW で頭打ち)。
	// 日付をタイトル直後の同じ桁に置くことで、広い端末でも右端まで間延びしない (0071)。
	titleColW := 0
	if rightCol > 0 {
		for _, t := range m.rows {
			if dw := dispWidth(blockedTitle(t)); dw > titleColW {
				titleColW = dw
			}
		}
		if titleColW > titleW {
			titleColW = titleW
		}
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
		line.WriteString(tuiDimStyle.Render(fmt.Sprintf("%-*s", tuiIDColW, t.ID)))
		line.WriteByte(' ')
		line.WriteString(statusStyle(t.Status).Render(fmt.Sprintf("%-*s", tuiStatusColW, t.Status)))
		line.WriteByte(' ')

		// SESSION 列 (in-progress 行があるときだけ出す。タイトルを侵食しない独立列) (0073)。
		// 各行は working/waiting/ended/?/(空) を sessW 桁に左寄せ。色は waiting を目立たせる (0070)。
		if sessW > 0 {
			if label := tuiSessionLabel(t); label == "" {
				line.WriteString(strings.Repeat(" ", sessW))
			} else {
				line.WriteString(tuiSessionStyle(label).Render(label))
				if pad := sessW - dispWidth(label); pad > 0 {
					line.WriteString(strings.Repeat(" ", pad))
				}
			}
			line.WriteByte(' ')
		}

		ttl := truncateDisp(blockedTitle(t), titleW)
		ttlDisp := dispWidth(ttl)
		if selected {
			line.WriteString(tuiBoldStyle.Render(ttl))
		} else {
			line.WriteString(ttl)
		}

		// UPDATED はタイトル列の右隣の固定桁にそろえる (右端寄せにしない → 間延びしない)。
		if rightCol > 0 {
			pad := titleColW - ttlDisp + 2 // タイトル列の後に 2 桁あけて日付
			if pad < 1 {
				pad = 1
			}
			line.WriteString(strings.Repeat(" ", pad))
			line.WriteString(tuiDimStyle.Render(displayDate(t.Updated)))
		}
		b.WriteString(line.String())
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return lipgloss.NewStyle().Width(w).Height(m.listH).MaxHeight(m.listH).Render(b.String())
}

// 一覧の固定列幅 (ポインタ/id/status/session)。renderList と幅計算 (listCols) で共有する。
const (
	tuiIDColW      = 4
	tuiStatusColW  = 11
	tuiSessionColW = 7 // 最長ラベル "waiting"/"working" の幅
)

// sessionColWidth は SESSION 列の幅を返す。in-progress 行が一つでもあれば tuiSessionColW、
// 無ければ 0 (列を出さない。CLI list の showSession と同じ)。
func (m *tuiModel) sessionColWidth() int {
	for _, t := range m.rows {
		if t.Status == "in-progress" {
			return tuiSessionColW
		}
	}
	return 0
}

// listCols は一覧の列構成を返す。cross は横断表示か (= project 列を出すか)、projW は project 列幅、
// sessW は SESSION 列幅 (0 = 非表示)、fixed は title より前の固定幅 (ポインタ + [project] + id +
// status + [session] + 余白)。
func (m *tuiModel) listCols() (cross bool, projW, sessW, fixed int) {
	cross = m.effProject == ""
	if cross {
		for _, t := range m.rows {
			if dw := dispWidth(t.Project); dw > projW {
				projW = dw
			}
		}
		projW = clampInt(projW, 0, 16)
	}
	sessW = m.sessionColWidth()
	// 行構成: "❯ " + [project] + id + " " + status + " " + [session + " "] + title
	fixed = 2 + tuiIDColW + 1 + tuiStatusColW + 1
	if cross {
		fixed += projW + 1
	}
	if sessW > 0 {
		fixed += sessW + 1
	}
	return
}

// updatedColWidth は UPDATED 列の表示幅 (行中の最大)。行が無ければ 0。
func (m *tuiModel) updatedColWidth() int {
	mx := 0
	for _, t := range m.rows {
		if dw := dispWidth(displayDate(t.Updated)); dw > mx {
			mx = dw
		}
	}
	return mx
}

// listNaturalWidth は全タイトルが切れずに収まる一覧の理想幅。横分割でリスト幅を
// ここまで広げ、固定上限による不要な truncate を避ける (layout で使う)。
func (m *tuiModel) listNaturalWidth() int {
	_, _, _, fixed := m.listCols()
	maxTitle := 0
	for _, t := range m.rows {
		if dw := dispWidth(blockedTitle(t)); dw > maxTitle {
			maxTitle = dw
		}
	}
	w := fixed + maxTitle
	if upd := m.updatedColWidth(); upd > 0 {
		w += upd + 2
	}
	return w
}

// tuiSessionLabel は in-progress タスクのセッション状態ラベルを返す (list の SESSION 列と同じ語)。
// in-progress 以外は ""、マーカー未取得 (hook 未導入など) は "?"。
func tuiSessionLabel(t Task) string {
	if t.Status != "in-progress" {
		return ""
	}
	st, ok := taskSessionState(t)
	if !ok {
		return "?"
	}
	switch st.State {
	case sessWaiting:
		return "waiting"
	case sessWorking:
		return "working"
	case sessEnded:
		return "ended"
	}
	return ""
}

// tuiSessionStyle はセッションラベルの色。list の sessionCell と同じ基準で、入力待ち
// (waiting) を目立たせ、working は cyan、ended / 未取得 (?) は淡色にする。
func tuiSessionStyle(label string) lipgloss.Style {
	switch label {
	case "waiting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true) // review 色 + 太字で目立たせる
	case "working":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	default: // "ended" / "?"
		return tuiDimStyle
	}
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
	// 先頭にライブのセッション状態を出す (in-progress のみ。frontmatter の session URL とは別の、
	// hook 由来の working/waiting/ended)。どの pane が応答待ちかを詳細でも確認できる (0070)。
	if label := tuiSessionLabel(t); label != "" {
		out = "セッション状態: " + label + "\n\n" + out
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
