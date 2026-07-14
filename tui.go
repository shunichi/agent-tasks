package main

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	var filterStatus string
	var filterProjects []string
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
			filterProjects = append(filterProjects, v)
		case "--projects":
			v, err := s.value("--projects")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, splitProjects(v)...)
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
	effProjects, _ := resolveListScope(filterProjects, allProjects, current)

	m := &tuiModel{
		dir:           storeDir(),
		effProjects:   effProjects,
		current:       current,
		projectPinned: len(filterProjects) > 0, // --project 明示時は p トグルで横断に切り替えない
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
	effProjects   []string // 実効 project スコープ集合 (空 = 横断、複数 = 部分集合横断)
	current       string   // 現在 project (p トグルの戻り先)
	projectPinned bool     // --project 明示。p トグルを無効化する
	filterStatus  string   // "" = 全 status
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

	// 選択 + アーカイブ (書き込み操作)。selected は Space でトグルするマルチセレクトで、
	// project/id キーで再読込 (ポーリング) や絞り込み変更をまたいで保持する (cursor と同じ方針)。
	// confirming 中は x の確認プロンプトを出し、y=実行 / n・Esc=中止。confirmTargets はその
	// 時点の対象スナップショット (以降の再読込で対象が変わらないよう固定する)。
	selected       map[string]bool
	confirming     bool
	confirmTargets []Task

	// liveKeys は「link された session_id が今 herdr にいる (= ライブな) タスク」の taskKey 集合。
	// 自分/他 pane を問わず、今 herdr 内で実体のあるセッションと結びついたタスクを一覧で示す。
	// herdr 状態はストア mtime と無関係に変わるので、reload だけでなく tick でも更新する
	// (refreshLiveTasks)。renderList は毎フレーム参照するだけ (link 読取・herdr 呼び出しをしない)。
	liveKeys map[string]bool

	searching     bool   // 検索入力モード中か (/ で開始、Enter 確定 / Esc 解除)
	searchQuery   string // 検索クエリ (タイトル部分一致、大小無視)。空 = フィルタ無し
	searchContent bool   // 本文も検索対象にするか (Tab でトグル)
	vertical      bool   // 詳細を下に積む縦分割か (狭い/縦長端末。広いと横分割で右に出す)
	vp            viewport.Model
	ready         bool
	width         int
	height        int
	sig           uint64    // ストアの変更検知シグネチャ
	updated       time.Time // 最終再読込時刻
	loadErr       error
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
	m.pruneSelection()
	m.applyFilter()
	m.refreshLiveTasks()
}

// refreshLiveTasks は「link された session_id が今 herdr にいるタスク」の集合を作り直す。
// herdr の全 agent スナップショット (session_id → status。TTL キャッシュ済み) と各タスクの link を
// 突合する (状態算出 taskSessionState と同じ経路)。herdr 外や取得失敗なら空にする (印なし = degrade)。
//
// 速度: herdrStateSnapshot は TTL キャッシュ越しなので `herdr agent list` の実呼び出しは高々 1 回/秒。
// link 読取はスコープ内タスク数ぶんの小さな JSON 読取で、tick (既定 2s) ごとに走らせても軽い。
// ライブ性は herdr 状態に連動するので、ストア不変 (mtime 同じ) でも tick で呼んで更新する。
func (m *tuiModel) refreshLiveTasks() {
	snap, ok := herdrStateSnapshot()
	if !ok || len(snap) == 0 {
		m.liveKeys = nil
		return
	}
	var live map[string]bool
	for _, t := range m.all {
		if liveSessionID(t, snap) != "" {
			if live == nil {
				live = map[string]bool{}
			}
			live[taskKey(t)] = true
		}
	}
	m.liveKeys = live
}

// liveSessionID は task の link session_id のうち snap (今 herdr にいる session_id 集合) に
// 含まれるものを 1 つ返す (無ければ "")。snap のキーは agent_session.value が非空の pane なので、
// メンバーシップ = herdr 内にライブなセッションがあることを意味する (status の値は問わない)。
func liveSessionID(t Task, snap map[string]string) string {
	key := taskSessionKey(t)
	if key == "" {
		return ""
	}
	link, ok := readSessionLink(key)
	if !ok {
		return ""
	}
	for _, sid := range linkSessionIDs(link) {
		if sid == "" {
			continue
		}
		if _, alive := snap[sid]; alive {
			return sid
		}
	}
	return ""
}

// pruneSelection は既にストアから消えた (別セッションでアーカイブ/完了移動された等) タスクの
// 選択を落とす。選択は絞り込みをまたいで保持する (隠れていても選択のまま) ので、可視行 (rows) では
// なく全タスク (all) の存在で判定する。
func (m *tuiModel) pruneSelection() {
	if len(m.selected) == 0 {
		return
	}
	exist := make(map[string]bool, len(m.all))
	for _, t := range m.all {
		exist[taskKey(t)] = true
	}
	for k := range m.selected {
		if !exist[k] {
			delete(m.selected, k)
		}
	}
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
		if !matchProjects(t.Project, m.effProjects) {
			continue
		}
		if !matchQuery(t, m.searchQuery, m.searchContent) {
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
			m.reload() // reload 内で refreshLiveTasks も呼ばれる
		} else {
			m.refreshLiveTasks() // ストア不変でも herdr 状態は変わるのでライブ性だけ更新する
		}
		return m, tuiTick(m.interval)

	case copyResultMsg:
		if msg.err != nil {
			m.flash = "コピー失敗: " + msg.err.Error()
		} else {
			m.flash = "コピーしました: " + msg.text
		}
		return m, nil

	case openResultMsg:
		if msg.err != nil {
			m.flash = msg.what + " を開けません: " + msg.err.Error()
		} else {
			m.flash = fmt.Sprintf("%s を開きました: %d 件", msg.what, msg.n)
		}
		return m, nil

	case archiveSyncMsg:
		// アーカイブ後の非同期 scoped sync の結果。ローカルの移動と一覧反映は doArchive の
		// 発火時に済んでいるので、ここでは同期結果を flash に出すだけ (失敗しても移動は完了済みで、
		// 後で手動 sync すれば取り込める)。
		if msg.err != nil {
			m.flash = "アーカイブ済み。同期は失敗 (後で sync してください): " + firstLine(msg.err.Error())
		} else {
			m.flash = "アーカイブして同期しました: " + strings.Join(msg.lines, " / ")
		}
		return m, nil

	case spawnResultMsg:
		// S キーの spawn の結果。子セッションは別 pane で起動済み (fire-and-forget) なので、
		// ここでは成否を flash に出すだけ。worktree 作成・追跡は子の start が行う。
		if msg.err != nil {
			m.flash = "spawn 失敗: " + firstLine(msg.err.Error())
		} else {
			m.flash = fmt.Sprintf("spawn しました: %s (pane %s)", msg.label, msg.pane)
		}
		return m, nil

	case focusResultMsg:
		// f キーの pane フォーカスの結果。成功時は対象 pane が前面に出る (この TUI は背面に
		// 回る) ので、戻ってきたとき用に flash を残す。特定できなければ理由を出す。
		if msg.err != nil {
			m.flash = "pane へ移動できません: " + firstLine(msg.err.Error())
		} else {
			m.flash = fmt.Sprintf("pane %s へ移動しました", msg.pane)
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
		// アーカイブ確認モード: y=実行 / n・Esc・q=中止 のみ受ける (誤操作防止)。
		// 実行時はローカルの移動を同期で行って即座に一覧へ反映し、git の scoped sync だけを
		// 非同期 (tea.Cmd) に回して UI をブロックしない。
		if m.confirming {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "y", "Y":
				m.confirming = false
				targets := m.confirmTargets
				m.confirmTargets = nil
				return m, m.doArchive(targets)
			case "n", "N", "esc", "q":
				m.confirming = false
				m.confirmTargets = nil
				m.flash = "アーカイブを中止しました"
			}
			return m, nil
		}
		// 検索入力モード: 入力を searchQuery に取り込み、インクリメンタルに絞り込む。
		// Enter で確定 (フィルタは維持)、Esc で解除、Tab で本文検索トグル。
		if m.searching {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.searching = false // 確定 (絞り込みは残す)
			case "esc":
				m.searching = false
				m.searchQuery = ""
				m.applyFilter()
			case "backspace":
				if r := []rune(m.searchQuery); len(r) > 0 {
					m.searchQuery = string(r[:len(r)-1])
					m.applyFilter()
				}
			case "tab":
				m.searchContent = !m.searchContent // タイトルのみ ↔ タイトル+本文
				m.applyFilter()
			default:
				if msg.Type == tea.KeyRunes {
					m.searchQuery += string(msg.Runes)
					m.applyFilter()
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "/":
			m.searching = true // 検索入力モードへ
			return m, nil
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
		case "up", "ctrl+p":
			m.cursor--
			m.clampCursor()
			m.syncDetail()
		case "down", "ctrl+n":
			m.cursor++
			m.clampCursor()
			m.syncDetail()
		case "k":
			// tig 風: 詳細表示中は詳細を 1 行スクロール、一覧のみなら選択を上へ。
			if m.showDetail {
				var cmd tea.Cmd
				m.vp, cmd = m.vp.Update(tea.KeyMsg{Type: tea.KeyUp})
				return m, cmd
			}
			m.cursor--
			m.clampCursor()
			m.syncDetail()
		case "j":
			if m.showDetail {
				var cmd tea.Cmd
				m.vp, cmd = m.vp.Update(tea.KeyMsg{Type: tea.KeyDown})
				return m, cmd
			}
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
		case "S":
			// 選択タスクを別 pane で spawn (別セッションを開いて start させる)。fire-and-forget。
			// 事前条件 (現在リポジトリのタスクか / 着手対象か) は spawnAction で判定し、実際の
			// herdr 起動は外部プロセスを伴うので tea.Cmd で非同期実行して UI をブロックしない。
			if t, ok := m.selectedTask(); ok {
				if proceed, msg := spawnAction(t, m.current); !proceed {
					m.flash = msg
					return m, nil
				}
				m.flash = "spawn 中…"
				return m, spawnCmd(t)
			}
			return m, nil
		case "f":
			// 選択タスクを実行中の herdr pane にフォーカスを移す (別 pane で spawn した作業へ飛ぶ)。
			// pane 特定は list の SESSION 列と同じ突合 (session-link → herdr agent list)。herdr 内のみ。
			// herdr へのシェルアウトを伴うので tea.Cmd で非同期実行し、結果を focusResultMsg で受ける。
			if t, ok := m.selectedTask(); ok {
				m.flash = "pane へ移動中…"
				return m, focusCmd(t)
			}
			return m, nil
		case "p":
			if !m.projectPinned && m.current != "" {
				if len(m.effProjects) == 0 {
					m.effProjects = []string{m.current} // 横断 → 現在 project のみ
				} else {
					m.effProjects = nil // → 横断
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
		case "o":
			// 選択タスクの PR (prs:) を既定ブラウザで開く (複数あれば全部)。ブラウザ起動は
			// 外部コマンドなので tea.Cmd で非同期実行し、結果を openResultMsg で受ける。
			if t, ok := m.selectedTask(); ok {
				urls, msg := prBrowserAction(t)
				if msg != "" {
					m.flash = msg
					return m, nil
				}
				return m, openCmd("PR", urls)
			}
			return m, nil
		case "O":
			// 選択タスクのセッション URL (session: = claude.ai の Claude Code セッション) を
			// 既定ブラウザで開く。o (PR) と対の導線。session: が URL でなければフラッシュのみ。
			if t, ok := m.selectedTask(); ok {
				urls, msg := sessionBrowserAction(t)
				if msg != "" {
					m.flash = msg
					return m, nil
				}
				return m, openCmd("セッション", urls)
			}
			return m, nil
		case "t":
			// 選択タスクの tracker (tracker: = 外部 issue tracker / 課題管理の URL、複数可) を
			// 既定ブラウザで開く。o (PR) / O (セッション) と対の導線。空 / URL でなければフラッシュのみ。
			if t, ok := m.selectedTask(); ok {
				urls, msg := trackerBrowserAction(t)
				if msg != "" {
					m.flash = msg
					return m, nil
				}
				return m, openCmd("tracker", urls)
			}
			return m, nil
		case "r":
			m.reload()
		case " ":
			// Space: 選択トグル (マルチセレクト)。選択は project/id で保持され、絞り込み・再読込を
			// またいで残る。x で選択中の全タスクを一括アーカイブできる。
			if t, ok := m.selectedTask(); ok {
				k := taskKey(t)
				if m.selected[k] {
					delete(m.selected, k)
				} else {
					if m.selected == nil {
						m.selected = map[string]bool{}
					}
					m.selected[k] = true
				}
			}
		case "x":
			// x: アーカイブ。選択があれば選択中の全タスク、無ければカーソル行の 1 件を対象に、
			// 件数を添えた確認プロンプトを出す (実行は y)。
			targets := m.archiveTargets()
			if len(targets) == 0 {
				m.flash = "アーカイブ対象がありません"
				return m, nil
			}
			m.confirming = true
			m.confirmTargets = targets
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

// archiveTargets は x キーでアーカイブする対象を決める。Space で選択 (マルチセレクト) が
// あれば選択中の全タスク (絞り込みで隠れていても選択は保持されるので all から解決)、無ければ
// カーソル行の 1 件。選択・カーソルとも無ければ nil。
func (m *tuiModel) archiveTargets() []Task {
	if len(m.selected) > 0 {
		var out []Task
		for _, t := range m.all {
			if m.selected[taskKey(t)] {
				out = append(out, t)
			}
		}
		return out
	}
	if t, ok := m.selectedTask(); ok {
		return []Task{t}
	}
	return nil
}

// archiveSyncMsg はアーカイブ後の非同期 scoped sync の結果 (人間向け結果行と成否)。
type archiveSyncMsg struct {
	lines []string
	err   error
}

// doArchive は targets をローカルでアーカイブ (<project>/archive/ へ移動) して即座に一覧へ
// 反映し、git への反映 (scoped sync) を行う tea.Cmd を返す (push を含むので非同期にして UI を
// ブロックしない)。移動は os.Rename でアトミック。一部が失敗しても成功分は反映し、失敗は
// flash で知らせる。移動対象が 0 件なら nil (sync 不要)。
func (m *tuiModel) doArchive(targets []Task) tea.Cmd {
	moved := 0
	var paths []string // scoped sync 用: 各タスクの from(旧) と to(新) の store 相対パス
	var errs []error
	for _, t := range targets {
		dest, err := moveTaskToArchive(t.Project, t.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", t.Project, t.ID, err))
			continue
		}
		moved++
		delete(m.selected, taskKey(t))
		paths = append(paths, storeRel(t.Path), storeRel(dest))
	}
	m.reload() // 移動済みファイルは一覧から消える (loadTasks は archive/ を読まない)
	if len(errs) > 0 {
		m.flash = fmt.Sprintf("%d 件アーカイブ (一部失敗: %v)", moved, errors.Join(errs...))
	} else {
		m.flash = fmt.Sprintf("%d 件アーカイブしました。同期中…", moved)
	}
	if len(paths) == 0 {
		return nil
	}
	dir := m.dir
	return func() tea.Msg {
		lines, err := syncStore(dir, paths, true)
		return archiveSyncMsg{lines: lines, err: err}
	}
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

// spawnAction は S キー (選択タスクを別 pane で spawn) の事前条件を判定する (テスト用に純粋化)。
// spawn は起動元 (cwd) のメインリポ root で子 pane を開くため、対象タスクは現在のリポジトリ
// (current) のものに限る (別 project は worktree の場所を特定できない)。また spawn は着手用なので
// 完了済み (done) は除外する。proceed=true なら spawn 可、false なら msg を flash に出して中止する。
// in-progress + session ありの二重着手ガードは spawnTask 側で判定し、失敗として flash に出す
// (TUI からは --force を渡せないため)。
func spawnAction(t Task, current string) (proceed bool, msg string) {
	if current == "" {
		return false, "spawn は git リポジトリ内で tui を起動したときのみ使えます"
	}
	if t.Project != current {
		return false, fmt.Sprintf("spawn は現在のリポジトリ (%s) のタスクのみ対応です (%s は該当リポジトリの pane から)", current, t.Project)
	}
	if t.Status == "done" {
		return false, "完了済みのタスクです (spawn は着手用)"
	}
	return true, ""
}

// spawnResultMsg は非同期 spawn の結果 (起動した子のラベルと pane、成否)。Update が flash 表示に使う。
type spawnResultMsg struct {
	label string
	pane  string
	err   error
}

// spawnCmd は選択タスクの spawn (別 pane で新セッションを開いて start させる) を非同期で行い、
// 結果を spawnResultMsg で返す tea.Cmd。herdr へのシェルアウトを伴うので UI をブロックしない。
// split=down / focus=false (背面起動で TUI にフォーカスを残す) / force=false (二重着手ガードを尊重)。
func spawnCmd(t Task) tea.Cmd {
	return func() tea.Msg {
		pane, err := spawnTask(t, "down", false, false)
		if err != nil {
			return spawnResultMsg{err: err}
		}
		return spawnResultMsg{label: spawnLabel(t), pane: pane.PaneID}
	}
}

// focusResultMsg は非同期 focus (f キー) の結果。成功時は移動先 pane、失敗時はエラー。
// Update が flash 表示に使う。
type focusResultMsg struct {
	pane string
	err  error
}

// focusCmd は選択タスクを実行中の herdr pane にフォーカスを移す処理を非同期で行い、結果を
// focusResultMsg で返す tea.Cmd。herdr へのシェルアウトを伴うので UI をブロックしない。
// pane 特定 (session-link → herdr agent list) と focus は focusTaskPane が担う (CLI の focus と共有)。
func focusCmd(t Task) tea.Cmd {
	return func() tea.Msg {
		pane, err := focusTaskPane(t)
		return focusResultMsg{pane: pane, err: err}
	}
}

// prBrowserAction は o キー (PR をブラウザで開く) の振る舞いを決める (テスト用に純粋化)。
// 開くべき PR URL 一覧を返す。PR が無ければ nil と表示メッセージを返す。
func prBrowserAction(t Task) (urls []string, msg string) {
	if len(t.PRs) == 0 {
		return nil, "このタスクに PR はありません"
	}
	return t.PRs, ""
}

// sessionBrowserAction は O キー (セッション URL をブラウザで開く) の振る舞いを決める
// (テスト用に純粋化)。session: が http(s) URL のときそれを返す。空/URL でなければ nil と
// 表示メッセージを返す。session: は start (session-link) が claude.ai の web URL を記録する。
func sessionBrowserAction(t Task) (urls []string, msg string) {
	if isHTTPURL(t.Session) {
		return []string{t.Session}, ""
	}
	if strings.TrimSpace(t.Session) == "" {
		return nil, "このタスクにセッション URL はありません"
	}
	return nil, "session 欄が URL ではありません"
}

// trackerBrowserAction は t キー (tracker: をブラウザで開く) の振る舞いを決める
// (テスト用に純粋化)。tracker: の http(s) URL を全て返す。空なら nil とメッセージ、
// 値はあるが URL 形式が無ければその旨のメッセージを返す (任意ホストを許すが、開けるのは
// http(s) のみ。doctor が URL 形式を別途検査する)。
func trackerBrowserAction(t Task) (urls []string, msg string) {
	if len(t.Tracker) == 0 {
		return nil, "このタスクに tracker はありません"
	}
	for _, u := range t.Tracker {
		if isHTTPURL(u) {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return nil, "tracker 欄が URL ではありません"
	}
	return urls, ""
}

// openResultMsg は非同期のブラウザ起動の結果 (何を・何件開いたか・成否)。
// what はフラッシュ表示用の対象名 ("PR" / "セッション" / "tracker")。
type openResultMsg struct {
	what string
	n    int
	err  error
}

// openCmd は urls を既定ブラウザで開く処理を非同期で行い、結果を openResultMsg で返す。
// what はフラッシュ表示に使う対象名 (PR / セッション)。
func openCmd(what string, urls []string) tea.Cmd {
	return func() tea.Msg {
		return openResultMsg{what: what, n: len(urls), err: openURLs(urls)}
	}
}

// openURLs は各 URL を OS の既定ブラウザで開く。TUI は alt-screen なので、外部プロセスは
// 起動だけして待たず (フォーカスを奪われない)、バックグラウンドで後始末する。
// 対応コマンドが無ければエラー (呼び出し側がフッターに表示)。
func openURLs(urls []string) error {
	opener, ok := browserOpener()
	if !ok {
		return fmt.Errorf("ブラウザを開くコマンドが見つかりません (xdg-open/open/wslview)")
	}
	for _, u := range urls {
		cmd := exec.Command(opener, u)
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait() // opener は即座にブラウザへ委譲して終了する。ゾンビ化を防ぐため後始末。
	}
	return nil
}

// browserOpener は PATH にある URL オープナーを返す (最初に見つかったもの)。
func browserOpener() (string, bool) {
	for _, n := range []string{
		"xdg-open", // Linux (freedesktop)
		"open",     // macOS
		"wslview",  // WSL (wslu)
	} {
		if _, err := exec.LookPath(n); err == nil {
			return n, true
		}
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
	// 選択を供給し続けて常駐するツール (wl-copy / xclip) は、tmux popup 内で起動すると
	// popup のプロセスグループに属し、popup が閉じるときに kill される → クリップボードが
	// 空になる。Setsid で新セッション (別プロセスグループ) に切り離し、親端末が閉じても
	// 常駐が生き残るようにする (nohup/setsid 相当。速やかに終了するツールには無害)。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
		// 横分割: 一覧 (左) / 区切り線 / 詳細 (右)。区切り線は listH 行ちょうどにする
		// (末尾に改行を残すと 1 行増え、body が listH+1 行になって View 全体が height+1 行に
		// なり、実端末では最上部のヘッダが 1 行分スクロールで押し出されて消える)。
		sep := tuiSepStyle.Height(m.listH).Render(strings.TrimSuffix(strings.Repeat("│\n", m.listH), "\n"))
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
	// dim 系は ANSI faint (SGR 2) を使う。従来の固定色 Color("8") (bright black) は端末/テーマに
	// よっては暗すぎて潰れる (ID・ヘッダーが読めない: 0126)。faint はデフォルト前景を減光するので
	// 端末の配色に追従して読める。CLI 側 (render.go の dim = "\033[2m") とも揃う。
	tuiFooterStyle = lipgloss.NewStyle().Faint(true)
	tuiDimStyle    = lipgloss.NewStyle().Faint(true)
	tuiSepStyle    = lipgloss.NewStyle().Faint(true)
	tuiPointStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	tuiBoldStyle   = lipgloss.NewStyle().Bold(true)
	tuiSelectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // 選択マーカー (yellow)
	tuiLiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // ライブセッション印 ● (green)
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
	scope := "全 project"
	if len(m.effProjects) > 0 {
		scope = strings.Join(m.effProjects, ",")
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
	// ヘッダの状態情報 (scope/status/件数/時刻) はデフォルト前景で描く。ユーザーが読む情報なので
	// dim にしない (bold のタイトルが先頭に立つので階層は保てる)。0126: 従来は dim で暗く読めなかった。
	info := fmt.Sprintf("  %s  status:%s  done:%s  %d件  %s",
		scope, filt, done, len(m.rows), m.updated.Format("15:04:05"))
	line := left + info
	if n := len(m.selected); n > 0 {
		line += tuiSelectStyle.Render(fmt.Sprintf("  選択:%d", n))
	}
	// 今 herdr にライブなセッションを持つ「可視行」の数 (● の凡例も兼ねる)。行に出す ● と一致させる
	// ため、絞り込みで隠れているライブは数えない。0 のときは出さない。
	if n := m.visibleLiveCount(); n > 0 {
		line += tuiLiveStyle.Render(fmt.Sprintf("  ●ライブ:%d", n))
	}
	if m.searching || m.searchQuery != "" {
		target := "title"
		if m.searchContent {
			target = "title+本文"
		}
		q := m.searchQuery
		if m.searching {
			q += "▌" // 入力中はカーソルを添える
		}
		line += tuiPointStyle.Render(fmt.Sprintf("  /%s [%s]", q, target))
	}
	if m.loadErr != nil {
		line += statusStyle("blocked").Render("  読み取りエラー")
	}
	return tuiTrunc(line, m.width)
}

func (m *tuiModel) renderFooter() string {
	// アーカイブ確認中は最優先で件数付きプロンプトを出す (list/detail の別に関わらず)。
	if m.confirming {
		n := len(m.confirmTargets)
		prompt := fmt.Sprintf("%d 件をアーカイブします (一覧から退避)。 y=実行  n/Esc=中止", n)
		return tuiTrunc(tuiSelectStyle.Render(prompt), m.width)
	}
	var keys string
	switch {
	case m.showHelp:
		keys = "?/q/Esc 閉じる"
	case m.searching:
		keys = "文字入力で絞り込み  Tab 本文検索切替  Enter 確定  Esc 解除"
	case m.showDetail:
		keys = "↑↓/^n^p 選択  jk 行  Space 選択  x アーカイブ  / 検索  c コピー  S spawn  f 移動  o PR  a done  s status  p project  ? ヘルプ  q/Esc 詳細を閉じる"
	default:
		keys = "↑↓/jk 選択  Enter 詳細  Space 選択  x アーカイブ  / 検索  c コピー  S spawn  f 移動  o PR  a done  s status  p project  ? ヘルプ  q/Esc 終了"
	}
	// flash (コピー結果など) は footer 先頭に最優先で置く。ヘッダの status 情報を潰さず、
	// footer は別行なので狭い端末 (tmux popup / 縦分割の詳細表示) でも両方見える。狭くて
	// キー説明が truncate されても、先頭の flash は残る。
	if m.flash != "" {
		line := tuiPointStyle.Render(m.flash) + "  " + tuiFooterStyle.Render(keys)
		return tuiTrunc(line, m.width)
	}
	return tuiFooterStyle.Render(tuiTrunc(keys, m.width))
}

// helpEntries は表示する (キー, 説明) の一覧。renderHelp とテストで共有する。
func helpEntries() [][2]string {
	return [][2]string{
		{"↑/↓, Ctrl+n/Ctrl+p", "選択を上下に移動 (詳細表示中も)"},
		{"j / k", "一覧では選択移動、詳細表示中は詳細を 1 行スクロール"},
		{"g / G", "先頭 / 末尾へ"},
		{"Enter", "選択タスクの詳細を開く"},
		{"/", "検索 (タイトル部分一致。Tab で本文も対象、Esc 解除)"},
		{"PgUp/PgDn, K/J", "詳細をスクロール"},
		{"Ctrl+U / Ctrl+D", "詳細を半画面スクロール"},
		{"マウスホイール", "詳細をスクロール"},
		{"Space", "選択トグル (マルチセレクト。絞り込み/再読込をまたいで保持)"},
		{"x", "選択タスク (無ければカーソル行) をアーカイブ (確認あり)"},
		{"c", "選択タスクの start task <NNNN> をクリップボードへコピー"},
		{"S", "選択タスクを別 pane で spawn (別セッションで start。herdr 内のみ)"},
		{"f", "選択タスクを実行中の herdr pane にフォーカス (別 pane の作業へ飛ぶ。herdr 内のみ)"},
		{"o", "選択タスクの PR (prs:) を既定ブラウザで開く"},
		{"O", "選択タスクのセッション URL (claude.ai) を既定ブラウザで開く"},
		{"t", "選択タスクの tracker (tracker:) を既定ブラウザで開く"},
		{"a", "done タスクの表示 / 非表示を切替"},
		{"s", "status フィルタを循環 (全→todo→…→done)"},
		{"p", "現在 project のみ / 全 project を切替"},
		{"r", "ストアを今すぐ再読込 (通常は自動で更新)"},
		{"?", "このヘルプを開閉"},
		{"q / Esc", "詳細を閉じる / (一覧で) 終了"},
		{"Ctrl+C", "終了"},
		{"● (緑)", "その行のタスクが今 herdr にライブなローカルセッションを持つ (自分/他 pane 問わず)"},
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
	// どの commit 時点のバイナリで動いているか、ヘルプを開いたまま確認できるようにする。
	b.WriteString("\n")
	b.WriteString(tuiDimStyle.Render(formatVersion(readVCSInfo())))

	// 内容の必要幅 (最長行) を求める。行の内訳は
	// 「キー列 (keyW) + 区切り 2 桁 + 説明」/ 見出し / 末尾の淡色 2 行。
	natural := dispWidth("キーバインド")
	for _, e := range entries {
		if w := keyW + 2 + dispWidth(e[1]); w > natural {
			natural = w
		}
	}
	for _, s := range []string{"ストアの変更は自動で反映されます (r で即時)。", formatVersion(readVCSInfo())} {
		if w := dispWidth(s); w > natural {
			natural = w
		}
	}

	// 枠の内側幅 = 端末幅から枠 (2) + 左右パディング (2) を引いた幅。ターミナルが広ければ内容を
	// 折り返さずに収める。ただし内容の必要幅を超えては広げない (間延び防止)。狭い端末では端末幅に
	// 収め、収まらない説明は lipgloss が折り返す (既存の下限処理を維持)。
	// lipgloss の Width は左右パディング (Padding(0,1) の 2 桁) を内側に含むので、テキストの必要幅
	// natural に 2 を足したものが折り返さない最小の Width。
	innerW := m.width - 4
	if need := natural + 2; innerW > need {
		innerW = need
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

	cross, projW, sessW, liveW, fixed := m.listCols()
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
			if dw := dispWidth(displayTitle(t)); dw > titleColW {
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
		// 先頭 1 桁は選択マーカー (Space で付けたマルチセレクトの印)。未選択は空白。
		if m.selected[taskKey(t)] {
			line.WriteString(tuiSelectStyle.Render("*"))
		} else {
			line.WriteByte(' ')
		}
		line.WriteString(ptr)
		if cross {
			line.WriteString(tuiDimStyle.Render(padDisp(t.Project, projW)))
			line.WriteByte(' ')
		}
		// ID は一覧の主キー。CLI の list と同様デフォルト前景で描く (dim にしない。0126)。
		line.WriteString(fmt.Sprintf("%-*s", tuiIDColW, t.ID))
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

		// LIVE 印 (●) 列 (可視行に 1 件でもライブがあるときだけ出す。status 非依存)。link された
		// session_id が今 herdr 内にいるタスク = ライブ。緑の ● で「今 herdr で実体のある作業」を示す。
		if liveW > 0 {
			if m.liveKeys[taskKey(t)] {
				line.WriteString(tuiLiveStyle.Render("●"))
			} else {
				line.WriteString(strings.Repeat(" ", tuiLiveColW))
			}
			line.WriteByte(' ')
		}

		ttl := truncateDisp(displayTitle(t), titleW)
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
			line.WriteString(tuiDimStyle.Render(displayDateOr(t.Updated, t.Created)))
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
	tuiSessionColW = 7 // 最長ラベル "waiting"/"working"/"blocked" の幅
	tuiLiveColW    = 1 // ライブ印 ● の幅 (1 グリフ)
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

// liveColWidth は LIVE 印 (●) 列の幅を返す。可視行に 1 件でもライブなタスクがあれば tuiLiveColW、
// 無ければ 0 (列を出さない。SESSION 列と同じく「必要なときだけ出す」)。
func (m *tuiModel) liveColWidth() int {
	if len(m.liveKeys) == 0 {
		return 0
	}
	for _, t := range m.rows {
		if m.liveKeys[taskKey(t)] {
			return tuiLiveColW
		}
	}
	return 0
}

// visibleLiveCount は可視行 (rows) のうちライブなタスク数を返す (ヘッダの ●ライブ:N 用)。
func (m *tuiModel) visibleLiveCount() int {
	if len(m.liveKeys) == 0 {
		return 0
	}
	n := 0
	for _, t := range m.rows {
		if m.liveKeys[taskKey(t)] {
			n++
		}
	}
	return n
}

// listCols は一覧の列構成を返す。cross は横断表示か (= project 列を出すか)、projW は project 列幅、
// sessW は SESSION 列幅 (0 = 非表示)、liveW は LIVE 印列幅 (0 = 非表示)、fixed は title より前の
// 固定幅 (ポインタ + [project] + id + status + [session] + [live] + 余白)。
func (m *tuiModel) listCols() (cross bool, projW, sessW, liveW, fixed int) {
	// project 列は「単一 project に絞っているとき」以外は出す (横断=全件も、複数指定の部分集合も出す)。
	cross = len(m.effProjects) != 1
	if cross {
		for _, t := range m.rows {
			if dw := dispWidth(t.Project); dw > projW {
				projW = dw
			}
		}
		projW = clampInt(projW, 0, 16)
	}
	sessW = m.sessionColWidth()
	liveW = m.liveColWidth()
	// 行構成: sel(1) + "❯ " + [project] + id + " " + status + " " + [session + " "] + [live + " "] + title
	// 先頭 1 桁は選択マーカー (Space によるマルチセレクト。未選択時は空白) の固定ガター。
	fixed = 1 + 2 + tuiIDColW + 1 + tuiStatusColW + 1
	if cross {
		fixed += projW + 1
	}
	if sessW > 0 {
		fixed += sessW + 1
	}
	if liveW > 0 {
		fixed += liveW + 1
	}
	return
}

// updatedColWidth は UPDATED 列の表示幅 (行中の最大)。行が無ければ 0。
func (m *tuiModel) updatedColWidth() int {
	mx := 0
	for _, t := range m.rows {
		if dw := dispWidth(displayDateOr(t.Updated, t.Created)); dw > mx {
			mx = dw
		}
	}
	return mx
}

// listNaturalWidth は全タイトルが切れずに収まる一覧の理想幅。横分割でリスト幅を
// ここまで広げ、固定上限による不要な truncate を避ける (layout で使う)。
func (m *tuiModel) listNaturalWidth() int {
	_, _, _, _, fixed := m.listCols()
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
	if t.IsHuman() {
		return "-" // 人手タスクはセッションを持たない
	}
	st, ok := taskSessionState(t)
	if !ok {
		return "?"
	}
	switch st.State {
	case sessBlocked:
		return "blocked" // herdr: 承認/許可待ち = 要対応
	case sessWaiting:
		return "waiting" // フォールバック (マーカー由来): 入力/許可待ち
	case sessWorking:
		return "working"
	case sessIdle:
		return "idle" // herdr: 応答完了・入力待ち
	case sessEnded:
		return "ended"
	}
	return ""
}

// tuiSessionStyle はセッションラベルの色。list の sessionCell と同じ基準で、承認待ち (blocked) と
// 入力待ち (waiting) を目立たせ、working は cyan、idle / ended / 未取得 (?) は淡色にする。
func tuiSessionStyle(label string) lipgloss.Style {
	switch label {
	case "blocked":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red bold = 最も目立たせる
	case "waiting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true) // review 色 + 太字で目立たせる
	case "working":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	default: // "idle" / "ended" / "?"
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
	// link された session_id が今 herdr にいれば「ライブ」を出す (status 非依存。一覧の ● に対応)。
	// 一覧の緑 ● が何を指すかを詳細でも確認できる (別 pane のライブセッションも含む)。
	if snap, ok := herdrStateSnapshot(); ok {
		if sid := liveSessionID(t, snap); sid != "" {
			out = "herdr セッション: ライブ (" + sid + ")\n\n" + out
		}
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
