package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// storeModel は AGENT_TASKS_STORE を temp に向け、指定タスクを書いてから reload した TUI モデルを返す。
// アーカイブ (ファイル移動) を伴うテストで、実ファイルのあるモデルを用意するのに使う。
func storeModel(t *testing.T, tasks ...[3]string) (*tuiModel, string) {
	t.Helper()
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	for _, ts := range tasks {
		proj, id, status := ts[0], ts[1], ts[2]
		body := "---\nid: \"" + id + "\"\nproject: " + proj + "\nstatus: " + status +
			"\ntitle: task " + id + "\n---\n\n# 要件\n"
		writeTask(t, store, proj, "", id+"-slug.md", body)
	}
	m := &tuiModel{dir: store}
	m.reload()
	return m, store
}

// archiveTargets は選択があれば選択中の全タスク (隠れていても)、無ければカーソル行を返す。
func TestArchiveTargets(t *testing.T) {
	m, _ := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "todo"},
		[3]string{"alpha", "0003", "todo"},
	)
	m.applyFilter()

	// 選択なし → カーソル行 (先頭) の 1 件。
	got := m.archiveTargets()
	if len(got) != 1 || got[0].ID != "0001" {
		t.Fatalf("選択なしはカーソル行 1 件のはず: got %+v", got)
	}

	// 0002, 0003 を選択 → 選択中の 2 件 (カーソルは 0001 のまま)。
	m.selected = map[string]bool{"alpha/0002": true, "alpha/0003": true}
	got = m.archiveTargets()
	if len(got) != 2 {
		t.Fatalf("選択 2 件のはず: got %d", len(got))
	}
	if got[0].ID != "0002" || got[1].ID != "0003" {
		t.Fatalf("選択中の 0002,0003 が対象のはず: got %+v", got)
	}
}

// 選択は絞り込みで隠れても保持し、archiveTargets は隠れた選択も含む (all から解決)。
func TestArchiveTargetsIncludesHiddenSelection(t *testing.T) {
	m, _ := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "done"}, // done は既定で隠れる
	)
	m.selected = map[string]bool{"alpha/0002": true}
	m.applyFilter() // done は rows から外れるが selected は残る

	got := m.archiveTargets()
	if len(got) != 1 || got[0].ID != "0002" {
		t.Fatalf("隠れた選択 (done) も対象になるはず: got %+v", got)
	}
}

// pruneSelection はストアから消えたタスクの選択を落とし、残っているものは保つ。
func TestPruneSelection(t *testing.T) {
	m, _ := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
	)
	m.selected = map[string]bool{"alpha/0001": true, "alpha/9999": true}
	m.pruneSelection()
	if !m.selected["alpha/0001"] {
		t.Fatal("存在するタスクの選択は保つべき")
	}
	if m.selected["alpha/9999"] {
		t.Fatal("消えたタスクの選択は落とすべき")
	}
}

// doArchive は選択タスクを archive/ へ移動し、一覧から消し、選択を落とす。sync 用 tea.Cmd を返す。
func TestDoArchiveMovesAndClears(t *testing.T) {
	m, store := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "todo"},
	)
	m.applyFilter()
	m.selected = map[string]bool{"alpha/0002": true}

	targets := m.archiveTargets()
	cmd := m.doArchive(targets)

	// ファイルが archive/ へ移動している。
	if _, err := os.Stat(filepath.Join(store, "alpha", "0002-slug.md")); !os.IsNotExist(err) {
		t.Fatalf("元パスは消えているはず: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "alpha", archiveDirName, "0002-slug.md")); err != nil {
		t.Fatalf("archive/ へ移動しているはず: %v", err)
	}
	// 一覧から消え、選択も落ちる。
	for _, r := range m.rows {
		if r.ID == "0002" {
			t.Fatal("アーカイブ後は一覧に残らないはず")
		}
	}
	if len(m.selected) != 0 {
		t.Fatalf("アーカイブした選択は落ちるはず: %+v", m.selected)
	}
	// sync 用の tea.Cmd が返る (paths があるため)。実行はしない (git 不要)。
	if cmd == nil {
		t.Fatal("アーカイブ後は scoped sync の tea.Cmd を返すはず")
	}
}

// Space で選択トグル → x で確認 → y で実行、の一連を Update 経由で通す。
func TestTUIArchiveKeyFlow(t *testing.T) {
	m, store := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "todo"},
	)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Space でカーソル行 (0001) を選択。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !model.(*tuiModel).selected["alpha/0001"] {
		t.Fatal("Space で選択されるはず")
	}
	// もう一度 Space で解除。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if model.(*tuiModel).selected["alpha/0001"] {
		t.Fatal("再度 Space で解除されるはず")
	}

	// x で確認モードへ (選択なし → カーソル行 0001 が対象)。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	mm := model.(*tuiModel)
	if !mm.confirming || len(mm.confirmTargets) != 1 || mm.confirmTargets[0].ID != "0001" {
		t.Fatalf("x で 0001 の確認モードになるはず: confirming=%v targets=%+v", mm.confirming, mm.confirmTargets)
	}

	// n で中止 (移動しない)。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if model.(*tuiModel).confirming {
		t.Fatal("n で確認モードを抜けるはず")
	}
	if _, err := os.Stat(filepath.Join(store, "alpha", "0001-slug.md")); err != nil {
		t.Fatalf("中止したので 0001 は残るはず: %v", err)
	}

	// 再度 x → y で実行。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if model.(*tuiModel).confirming {
		t.Fatal("y で確認モードを抜けるはず")
	}
	if _, err := os.Stat(filepath.Join(store, "alpha", archiveDirName, "0001-slug.md")); err != nil {
		t.Fatalf("y でアーカイブされるはず: %v", err)
	}
	if cmd == nil {
		t.Fatal("y 実行後は scoped sync の tea.Cmd を返すはず")
	}
}

// 選択マーカーと確認プロンプトが描画される。
func TestTUIArchiveRendering(t *testing.T) {
	m, _ := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "todo"},
	)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 0001 を選択 → 一覧に選択マーカー "*" とヘッダに "選択:1" が出る。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	out := stripANSI(model.View())
	if !strings.Contains(out, "*") {
		t.Fatalf("選択マーカー(*)が描画されるはず:\n%s", out)
	}
	if !strings.Contains(out, "選択:1") {
		t.Fatalf("ヘッダに選択件数が出るはず:\n%s", out)
	}

	// x で確認 → フッタに件数付きプロンプトが出る。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	out = stripANSI(model.View())
	if !strings.Contains(out, "アーカイブします") || !strings.Contains(out, "y=実行") {
		t.Fatalf("確認プロンプトが描画されるはず:\n%s", out)
	}
}
