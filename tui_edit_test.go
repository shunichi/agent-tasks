package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ヘルプに e (エディタで開く) の項目がある。
func TestHelpHasEditKey(t *testing.T) {
	found := false
	for _, e := range helpEntries() {
		if e[0] == "e" {
			found = true
			break
		}
	}
	if !found {
		t.Error("helpEntries に e (エディタで開く) が無い")
	}
}

// editorCommand は AGENT_TASKS_EDITOR (等) で解決したエディタに path を渡す *exec.Cmd を組む。
func TestEditorCommandResolvesEditorAndPath(t *testing.T) {
	dir := t.TempDir()
	// stub エディタ (実在させて LookPath を通す)。
	stub := filepath.Join(dir, "myedit")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 引数付きエディタ指定も分解される (edit サブコマンドと同じ editorArgv の挙動)。
	t.Setenv("AGENT_TASKS_EDITOR", stub+" -w")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	target := filepath.Join(dir, "0001-x.md")
	c, err := editorCommand(target)
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if c.Path != stub {
		t.Errorf("エディタ bin = %q, want %q", c.Path, stub)
	}
	// argv = [bin, -w, target]。
	if !slices.Equal(c.Args, []string{stub, "-w", target}) {
		t.Errorf("argv = %v, want [%s -w %s]", c.Args, stub, target)
	}
}

// PATH にエディタが無ければ editorCommand はエラーを返す (editCmd がそれを editFinishedMsg で伝える)。
func TestEditorCommandNotFound(t *testing.T) {
	t.Setenv("AGENT_TASKS_EDITOR", "no-such-editor-xyzzy")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("PATH", t.TempDir()) // 空ディレクトリ = 見つからない

	if _, err := editorCommand("/tmp/x.md"); err == nil {
		t.Error("エディタが無いのにエラーにならなかった")
	}
}

// e キーを押すと選択タスクに対して editCmd が発火する (キーハンドラの配線確認)。
// エディタは未検出にして editFinishedMsg 経由でエラーを観測することで、ExecProcess を避けつつ
// 「e → editCmd(選択タスクの Path)」の経路が繋がっていることを確かめる。
func TestEditKeyTriggersEditCmd(t *testing.T) {
	t.Setenv("AGENT_TASKS_EDITOR", "no-such-editor-xyzzy")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("PATH", t.TempDir())

	m := &tuiModel{
		all:         []Task{{Project: "alpha", ID: "0001", Status: "todo", Title: "A", Path: "/store/alpha/0001-a.md"}},
		effProjects: []string{"alpha"},
	}
	m.applyFilter()
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd == nil {
		t.Fatal("e キーで tea.Cmd が返らない (editCmd が発火していない)")
	}
	if _, ok := cmd().(editFinishedMsg); !ok {
		t.Fatalf("e キーの cmd 結果 = %T, want editFinishedMsg", cmd())
	}
}

// editCmd はエディタ未検出のとき、エラー入りの editFinishedMsg を返す tea.Cmd になる。
func TestEditCmdReturnsErrorMsgWhenEditorMissing(t *testing.T) {
	t.Setenv("AGENT_TASKS_EDITOR", "no-such-editor-xyzzy")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("PATH", t.TempDir())

	m := &tuiModel{}
	cmd := m.editCmd("/tmp/x.md")
	if cmd == nil {
		t.Fatal("editCmd が nil を返した")
	}
	msg, ok := cmd().(editFinishedMsg)
	if !ok {
		t.Fatalf("msg 型 = %T, want editFinishedMsg", cmd())
	}
	if msg.err == nil {
		t.Error("エディタ未検出なのに editFinishedMsg.err が nil")
	}
}
