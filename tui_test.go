package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestCycleStatus(t *testing.T) {
	want := []string{"todo", "in-progress", "blocked", "review", "done", ""}
	got := ""
	for _, w := range want {
		got = cycleStatus(got)
		if got != w {
			t.Fatalf("cycleStatus 遷移が不正: got %q want %q", got, w)
		}
	}
	// 未知の値は全 (空) に倒す。
	if s := cycleStatus("nonsense"); s != "" {
		t.Fatalf("未知 status は空に倒すべき: got %q", s)
	}
}

func mkTasks() []Task {
	return []Task{
		{Project: "alpha", ID: "0001", Status: "todo", Title: "a1"},
		{Project: "alpha", ID: "0002", Status: "in-progress", Title: "a2"},
		{Project: "alpha", ID: "0003", Status: "done", Title: "a3"},
		{Project: "beta", ID: "0001", Status: "todo", Title: "b1"},
		{Project: "beta", ID: "0002", Status: "blocked", Title: "b2"},
	}
}

func TestApplyFilterProjectScope(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: "alpha"}
	m.applyFilter()
	// alpha のうち done は既定で隠れる → 0001, 0002 の 2 件。
	if len(m.rows) != 2 {
		t.Fatalf("alpha スコープで期待 2 件, got %d", len(m.rows))
	}
	for _, r := range m.rows {
		if r.Project != "alpha" {
			t.Fatalf("alpha 以外が混入: %s", r.Project)
		}
	}
}

func TestApplyFilterShowDone(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: "alpha", showDone: true}
	m.applyFilter()
	if len(m.rows) != 3 {
		t.Fatalf("showDone=true で alpha 全 3 件のはず, got %d", len(m.rows))
	}
}

func TestApplyFilterStatus(t *testing.T) {
	// status 明示時は done も隠さない (list と同じ規則)。横断スコープ。
	m := &tuiModel{all: mkTasks(), effProject: "", filterStatus: "done"}
	m.applyFilter()
	if len(m.rows) != 1 || m.rows[0].Title != "a3" {
		t.Fatalf("status=done で a3 のみのはず, got %d 件", len(m.rows))
	}
}

func TestApplyFilterCrossProject(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: ""}
	m.applyFilter()
	// done を除く全 project = 4 件。
	if len(m.rows) != 4 {
		t.Fatalf("横断で done 除く 4 件のはず, got %d", len(m.rows))
	}
}

func TestApplyFilterPreservesSelectionByKey(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: ""}
	m.applyFilter()
	// beta/0002 (blocked) を選択する。
	target := "beta/0002"
	m.cursor = -1
	for i, r := range m.rows {
		if taskKey(r) == target {
			m.cursor = i
		}
	}
	if m.cursor < 0 {
		t.Fatal("対象タスクが見つからない")
	}
	// 先頭に新規タスクを足して並びを変える → 選択は key で追従するはず。
	m.all = append([]Task{{Project: "aaa", ID: "0001", Status: "todo", Title: "new"}}, m.all...)
	m.applyFilter()
	if taskKey(m.rows[m.cursor]) != target {
		t.Fatalf("選択が key で保持されていない: got %q want %q", taskKey(m.rows[m.cursor]), target)
	}
}

func TestApplyFilterClampsWhenSelectionGone(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: "beta"}
	m.applyFilter()
	m.cursor = len(m.rows) - 1
	last := taskKey(m.rows[m.cursor])
	// 選択していたタスクを消す。
	var kept []Task
	for _, tk := range m.all {
		if taskKey(tk) != last {
			kept = append(kept, tk)
		}
	}
	m.all = kept
	m.applyFilter()
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		t.Fatalf("選択消失後に cursor が範囲外: cursor=%d rows=%d", m.cursor, len(m.rows))
	}
}

// TestViewDoesNotPanic は各種サイズ/キー操作で View が落ちないことを確認する
// (端末を起動できない CI でも描画ロジックの健全性を担保する)。
func TestViewDoesNotPanic(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: ""}
	m.applyFilter()
	var model tea.Model = m

	// 通常サイズでは一覧の行内容 (id / status / title) が実際に描画されること。
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := model.View()
	for _, want := range []string{"agent-tasks tui", "0001", "in-progress", "a1", "b2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("View に %q が描画されていない\n--- View ---\n%s", want, out)
		}
	}

	// 様々なウィンドウサイズで初期化 → 描画。極小サイズでも落ちないこと。
	for _, sz := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 40, Height: 8}, {Width: 10, Height: 3}, {Width: 0, Height: 0}} {
		model, _ = model.Update(sz)
		if out := model.View(); out == "" {
			t.Fatalf("View が空 (size %dx%d)", sz.Width, sz.Height)
		}
	}
	// 通常サイズに戻してから主要キーを順に流す。
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, k := range []string{"down", "down", "up", "j", "k", "G", "g", "a", "s", "p", "pgdown", "pgup", "r"} {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	if out := model.View(); !strings.Contains(out, "agent-tasks tui") {
		t.Fatal("ヘッダが描画されていない")
	}
}

// TestDetailToggleAndQuit は仕様: 起動直後はリストのみ / Enter で詳細表示 /
// 詳細表示中の q・Esc は詳細を閉じる / リストのみでの q・Esc は終了、を検証する。
func TestDetailToggleAndQuit(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: ""}
	m.applyFilter()
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if model.(*tuiModel).showDetail {
		t.Fatal("起動直後は詳細非表示のはず")
	}

	// リストのみで q → 終了コマンド (QuitMsg)。
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("リストのみでの q は終了コマンドを返すはず")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q が QuitMsg を返していない")
	}

	// Enter → 詳細表示。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.(*tuiModel).showDetail {
		t.Fatal("Enter で詳細表示になるはず")
	}

	// 詳細表示中の Esc → 詳細を閉じる (終了しない)。
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.(*tuiModel).showDetail {
		t.Fatal("詳細表示中の Esc は詳細を閉じるはず")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("詳細を閉じるだけで終了してはいけない")
		}
	}

	// 閉じた後の q → 今度は終了。
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("詳細を閉じた後の q は終了コマンドを返すはず")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q が QuitMsg を返していない (詳細クローズ後)")
	}
}

// TestDetailLayout は分割の向きの決定 (純関数) を検証する。一覧を自然幅にしたとき
// 詳細ペインに残る実効幅が読み幅 (tuiMinDetailWidth) 以上なら横分割、満たなければ
// 一覧を切り詰めず縦分割 (詳細を下) に倒す。
func TestDetailLayout(t *testing.T) {
	tests := []struct {
		name           string
		width, natural int
		wantVertical   bool
	}{
		{"広い端末+短いタイトル → 横分割", 180, 40, false},
		{"広い端末+長いタイトルでも両立 → 横分割", 200, 95, false},
		{"一覧が広く詳細を確保できない → 縦分割", 120, 135, true},
		{"幅が中途半端で詳細が最小未満 → 縦分割", 70, 40, true},
		{"境界: 詳細がちょうど最小 → 横分割", tuiMinListWidth + 3 + tuiMinDetailWidth, tuiMinListWidth, false},
		{"境界: 1 足りない → 縦分割", tuiMinListWidth + 3 + tuiMinDetailWidth - 1, tuiMinListWidth, true},
	}
	for _, tt := range tests {
		listW, detailW, vertical := detailLayout(tt.width, tt.natural)
		if vertical != tt.wantVertical {
			t.Errorf("%s: vertical=%v want %v (listW=%d detailW=%d)", tt.name, vertical, tt.wantVertical, listW, detailW)
		}
		if !vertical && detailW < tuiMinDetailWidth {
			t.Errorf("%s: 横分割なのに詳細幅が最小未満: detailW=%d", tt.name, detailW)
		}
	}
}

// TestHelpToggle はヘルプの開閉と、ヘルプ表示中の挙動を検証する:
// ? で開閉 / q・Esc で閉じる (終了しない) / 表示中は他キーが無効。
func TestHelpToggle(t *testing.T) {
	m := &tuiModel{all: mkTasks(), effProject: ""}
	m.applyFilter()
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	press := func(s string) tea.Cmd {
		var cmd tea.Cmd
		model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		return cmd
	}

	if model.(*tuiModel).showHelp {
		t.Fatal("起動直後はヘルプ非表示のはず")
	}

	// ? で開く。主要キーの説明が描画される。
	press("?")
	if !model.(*tuiModel).showHelp {
		t.Fatal("? でヘルプが開くはず")
	}
	out := model.View()
	for _, want := range []string{"キーバインド", "Enter", "status フィルタ", "agent-tasks tui"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ヘルプに %q が描画されていない\n--- View ---\n%s", want, out)
		}
	}

	// 表示中の他キー (down) は無効 = 選択も動かず、ヘルプも開いたまま。
	before := model.(*tuiModel).cursor
	press("down")
	if !model.(*tuiModel).showHelp {
		t.Fatal("ヘルプ表示中の他キーで閉じてはいけない")
	}
	if model.(*tuiModel).cursor != before {
		t.Fatalf("ヘルプ表示中は選択が動かないはず: %d → %d", before, model.(*tuiModel).cursor)
	}

	// ? を再度押すと閉じる。
	press("?")
	if model.(*tuiModel).showHelp {
		t.Fatal("? の再押下でヘルプが閉じるはず")
	}

	// q でも閉じる。かつ終了コマンドは返さない (リストへ戻るだけ)。
	press("?")
	cmd := press("q")
	if model.(*tuiModel).showHelp {
		t.Fatal("q でヘルプが閉じるはず")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("ヘルプを閉じるだけで終了してはいけない")
		}
	}

	// 極小サイズでヘルプを開いても落ちない / 空にならない。
	for _, sz := range []tea.WindowSizeMsg{{Width: 40, Height: 8}, {Width: 10, Height: 4}, {Width: 1, Height: 1}} {
		model, _ = model.Update(sz)
		model.(*tuiModel).showHelp = true
		if model.View() == "" {
			t.Fatalf("ヘルプ表示が空 (size %dx%d)", sz.Width, sz.Height)
		}
	}
}

// TestLayoutOrientation はモデル経由で分割の向きを検証する: 広い端末は横分割
// (詳細を右、リストは内容に応じて広がる)、一覧が広すぎて詳細を確保できない端末や
// 狭い端末は縦分割 (詳細を下、全幅)。
func TestLayoutOrientation(t *testing.T) {
	// 固定上限 64 を超えるが、広い端末なら詳細にも幅が残る中程度のタイトル (表示幅 80)。
	tasks := append(mkTasks(), Task{Project: "alpha", ID: "0009", Status: "todo",
		Title: strings.Repeat("ab", 40)})

	// 広い端末: 横分割。リストは固定上限 64 ではなく内容に応じて広がり、詳細にも幅が残る。
	wide := &tuiModel{all: tasks, effProject: ""}
	wide.applyFilter()
	var wm tea.Model = wide
	wm, _ = wm.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	wm, _ = wm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w := wm.(*tuiModel)
	if w.vertical {
		t.Fatal("広い端末では横分割のはず")
	}
	if w.leftW <= 64 {
		t.Fatalf("広い端末ではリストが内容に応じて 64 を超えて広がるはず: leftW=%d", w.leftW)
	}
	if w.vp.Width < tuiMinDetailWidth {
		t.Fatalf("横分割では詳細ペインに最小幅が残るはず: vpW=%d", w.vp.Width)
	}

	// 一覧の自然幅が広すぎて詳細に読み幅を確保できない端末: 縦分割 (詳細を下) に倒す。
	// (旧挙動: タイトルを切り詰めてでも横並び。新挙動: 切り詰めず下に積む。)
	squeeze := &tuiModel{all: append(mkTasks(), Task{Project: "alpha", ID: "0009",
		Status: "todo", Title: strings.Repeat("長い", 40)}), effProject: ""} // 表示幅 160
	squeeze.applyFilter()
	var sm tea.Model = squeeze
	sm, _ = sm.Update(tea.WindowSizeMsg{Width: 130, Height: 50})
	sm, _ = sm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !sm.(*tuiModel).vertical {
		t.Fatal("一覧が広く詳細を確保できないときは縦分割のはず")
	}

	// 狭い端末: 縦分割 (詳細を下)。リストは全幅、詳細は下に高さを持つ。
	narrow := &tuiModel{all: tasks, effProject: ""}
	narrow.applyFilter()
	var nm tea.Model = narrow
	nm, _ = nm.Update(tea.WindowSizeMsg{Width: 70, Height: 50})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	n := nm.(*tuiModel)
	if !n.vertical {
		t.Fatal("狭い端末では縦分割 (詳細を下) のはず")
	}
	if n.leftW != 70 {
		t.Fatalf("縦分割ではリストが全幅のはず: leftW=%d", n.leftW)
	}
	if n.vp.Height < 1 || n.listH < 1 || n.listH+n.vp.Height >= 50 {
		t.Fatalf("縦分割の高さ配分が不正: listH=%d vpH=%d", n.listH, n.vp.Height)
	}
}

// TestWrapDetailNoClip は詳細本文が viewport 幅で折り返され、どの行も幅を超えない
// (= 横方向に切り詰められない) ことを確認する。
func TestWrapDetailNoClip(t *testing.T) {
	m := &tuiModel{}
	m.vp.Width = 40
	long := strings.Repeat("word ", 60) // 折り返しが必要な長い 1 行
	out := m.wrapDetail(long)
	if !strings.Contains(out, "\n") {
		t.Fatal("長い行が折り返されていない")
	}
	for _, line := range strings.Split(out, "\n") {
		if dispWidth(line) > 40 {
			t.Fatalf("折り返し後も行が幅を超える: dispWidth=%d line=%q", dispWidth(line), line)
		}
	}
}

// TestListUpdatedColumnNotFarRight は UPDATED が右端寄せでなくタイトル直後の桁にそろうこと
// (広い端末で間延びしない = 0071) を確認する。
func TestListUpdatedColumnNotFarRight(t *testing.T) {
	const upd = "2026-07-01T10:00:00+09:00"
	tasks := []Task{
		{Project: "alpha", ID: "0001", Status: "todo", Title: "短い", Updated: upd},
		{Project: "alpha", ID: "0002", Status: "todo", Title: "短2", Updated: upd},
	}
	m := &tuiModel{all: tasks, effProject: "alpha"}
	m.applyFilter()
	var model tea.Model = m
	// リストのみモード (全幅) の広い端末。
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	out := model.View()
	date := displayDate(upd)

	found := false
	for _, line := range strings.Split(out, "\n") {
		plain := stripANSI(line)
		idx := strings.Index(plain, date)
		if idx < 0 {
			continue
		}
		found = true
		col := dispWidth(plain[:idx]) // 日付の開始桁
		if col > 60 {
			t.Fatalf("UPDATED が右端寄りで間延びしている (開始桁=%d, 幅=120): %q", col, plain)
		}
	}
	if !found {
		t.Fatal("UPDATED 日付を含む行が見つからない")
	}
}

// TestSessionColumnWidth は SESSION 列が in-progress 行のあるときだけ出ることを確認する。
func TestSessionColumnWidth(t *testing.T) {
	m := &tuiModel{rows: []Task{{Status: "todo"}, {Status: "done"}}}
	if m.sessionColWidth() != 0 {
		t.Fatalf("in-progress が無いとき SESSION 列は 0 のはず: got %d", m.sessionColWidth())
	}
	m.rows = []Task{{Status: "todo"}, {Status: "in-progress"}}
	if m.sessionColWidth() != tuiSessionColW {
		t.Fatalf("in-progress があるとき SESSION 列は %d のはず: got %d", tuiSessionColW, m.sessionColWidth())
	}
}

// TestSessionColumnDoesNotEatTitle は SESSION を独立列にしたことで、todo 行と in-progress 行で
// タイトル開始桁がそろう (セッション表示がタイトルを侵食しない) ことを確認する (0073)。
func TestSessionColumnDoesNotEatTitle(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	tasks := []Task{
		{Project: "alpha", ID: "0001", Status: "todo", Title: "AAA"},
		{Project: "alpha", ID: "0002", Status: "in-progress", Title: "BBB"},
	}
	m := &tuiModel{all: tasks, effProject: "alpha"}
	m.applyFilter()
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	out := model.View()

	colA, colB := -1, -1
	for _, line := range strings.Split(out, "\n") {
		p := stripANSI(line)
		if i := strings.Index(p, "AAA"); i >= 0 {
			colA = dispWidth(p[:i])
		}
		if i := strings.Index(p, "BBB"); i >= 0 {
			colB = dispWidth(p[:i])
		}
	}
	if colA < 0 || colB < 0 {
		t.Fatalf("タイトル行が見つからない (colA=%d colB=%d)", colA, colB)
	}
	if colA != colB {
		t.Fatalf("タイトル開始桁がそろっていない (SESSION 列がタイトルを侵食): todo=%d in-progress=%d", colA, colB)
	}
}

// TestSessionLabel はセッションラベルの分岐 (in-progress 以外は空 / マーカー未取得は ?) を確認する。
func TestSessionLabel(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir()) // 実マーカーを読まない
	if got := tuiSessionLabel(Task{Status: "todo"}); got != "" {
		t.Fatalf("in-progress 以外は空のはず: got %q", got)
	}
	if got := tuiSessionLabel(Task{Status: "in-progress"}); got != "?" {
		t.Fatalf("マーカー未取得は ? のはず: got %q", got)
	}
}

func writeTaskFile(t *testing.T, dir, project, name, body string) string {
	t.Helper()
	pd := filepath.Join(dir, project)
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(pd, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStoreSignatureDetectsChange(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "alpha", "0001-x.md", "---\nid: \"0001\"\nstatus: todo\n---\n# x\n")
	sig1 := storeSignature(dir)
	if sig1 == 0 {
		t.Fatal("シグネチャが 0 (走査失敗)")
	}
	// 同一内容の再走査は同じシグネチャ。
	if storeSignature(dir) != sig1 {
		t.Fatal("不変なのにシグネチャが変わった")
	}
	// サイズが変わる更新 → シグネチャが変わる。
	writeTaskFile(t, dir, "alpha", "0001-x.md", "---\nid: \"0001\"\nstatus: in-progress\n---\n# x changed\n")
	if storeSignature(dir) == sig1 {
		t.Fatal("内容変更後もシグネチャが同じ")
	}
	// 新規ファイル追加でも変わる。
	sig2 := storeSignature(dir)
	writeTaskFile(t, dir, "alpha", "0002-y.md", "---\nid: \"0002\"\nstatus: todo\n---\n# y\n")
	if storeSignature(dir) == sig2 {
		t.Fatal("ファイル追加後もシグネチャが同じ")
	}
}

func TestReloadReadsStore(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "alpha", "0001-x.md", "---\nid: \"0001\"\nproject: alpha\nstatus: todo\ntitle: x\n---\n# x\n")
	m := &tuiModel{dir: dir, effProject: "alpha"}
	m.reload()
	if len(m.rows) != 1 || m.rows[0].Title != "x" {
		t.Fatalf("reload で 1 件読めるはず, got %d 件", len(m.rows))
	}
	if m.sig == 0 {
		t.Fatal("reload 後にシグネチャ未設定")
	}
}
