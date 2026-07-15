package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// n → タイトル入力 → Ctrl+S の一連を Update 経由で通し、draft タスクが作られることを確認する。
func TestTUICreateKeyFlow(t *testing.T) {
	m, store := storeModel(t,
		[3]string{"alpha", "0001", "todo"},
		[3]string{"alpha", "0002", "todo"},
	)
	m.effProjects = []string{"alpha"} // 登録先 project を単一に絞る (git 外でも createProject が解決)

	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// n でフォームを開く。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !model.(*tuiModel).creating {
		t.Fatal("n で登録フォームが開くはず")
	}
	// タイトルを入力 (日本語のみ → slug は task にフォールバック)。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("新しいタスク")})
	// Ctrl+S で登録。
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	mm := model.(*tuiModel)
	if mm.creating {
		t.Fatal("登録後はフォームを閉じるはず")
	}
	if !strings.Contains(mm.flash, "簡易登録しました") {
		t.Fatalf("flash に登録結果が出るはず: %q", mm.flash)
	}
	// alpha は 0001/0002 があるので新規は 0003。日本語のみ title なので slug=task。
	path := filepath.Join(store, "alpha", "0003-task.md")
	got, err := parseTask(path)
	if err != nil {
		t.Fatalf("parseTask(%s): %v", path, err)
	}
	if !got.Draft {
		t.Error("作成されたタスクは Draft=true のはず")
	}
	if got.Title != "新しいタスク" || got.Status != "todo" {
		t.Errorf("title/status = %q/%q", got.Title, got.Status)
	}
	// 一覧に即反映され、[簡易] バッジ付きで出る。
	if !strings.Contains(stripANSI(model.View()), draftTitlePrefix) {
		t.Error("一覧に [簡易] バッジが描画されるはず")
	}
}

// タイトル未入力で Ctrl+S するとエラーを出してフォームに留まり、ファイルは作られない。
func TestTUICreateRequiresTitle(t *testing.T) {
	m, _ := storeModel(t, [3]string{"alpha", "0001", "todo"})
	m.effProjects = []string{"alpha"}

	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS}) // タイトル空のまま登録

	mm := model.(*tuiModel)
	if !mm.creating {
		t.Fatal("タイトル未入力なら登録せずフォームに留まるはず")
	}
	if mm.newErr == "" {
		t.Fatal("タイトル必須のエラーが出るはず")
	}
}

// Esc で登録を中止するとフォームを閉じ、ファイルは作られない。
func TestTUICreateCancel(t *testing.T) {
	m, store := storeModel(t, [3]string{"alpha", "0001", "todo"})
	m.effProjects = []string{"alpha"}

	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("捨てる")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if model.(*tuiModel).creating {
		t.Fatal("Esc でフォームを閉じるはず")
	}
	// 新規ファイル (0002-*) は作られていない。
	if matches, _ := filepath.Glob(filepath.Join(store, "alpha", "0002-*.md")); len(matches) != 0 {
		t.Fatalf("中止したのでファイルは作られないはず: %v", matches)
	}
}

// 登録先 project を判定できない (git 外 + 横断表示) ときはエラーにして作らない。
func TestTUICreateNoProject(t *testing.T) {
	m, _ := storeModel(t, [3]string{"alpha", "0001", "todo"})
	m.current = ""      // git 外
	m.effProjects = nil // 横断 (単一に絞られていない)

	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	mm := model.(*tuiModel)
	if !mm.creating || mm.newErr == "" {
		t.Fatalf("project 判定不能ならエラーで留まるはず: creating=%v err=%q", mm.creating, mm.newErr)
	}
}
