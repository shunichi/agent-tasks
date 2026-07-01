package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinWorktree(t *testing.T) {
	// 相対 (start が作る ../<project>--<NNNN>) は root の兄弟に解決される。
	if got := joinWorktree("/home/u/repo", "../repo--0001"); got != "/home/u/repo--0001" {
		t.Errorf("joinWorktree relative = %q, want /home/u/repo--0001", got)
	}
	// 絶対パスはそのまま (root は無視)。
	if got := joinWorktree("/home/u/repo", "/abs/elsewhere"); got != "/abs/elsewhere" {
		t.Errorf("joinWorktree abs = %q, want /abs/elsewhere", got)
	}
}

func TestResolveWorktreeDirAbsolute(t *testing.T) {
	got, err := resolveWorktreeDir("/tmp/some/worktree/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/some/worktree" {
		t.Errorf("= %q, want /tmp/some/worktree (clean)", got)
	}
}

// cmdOpen は worktree を editorArgv のエディタで開く。stub エディタで、実在する worktree の
// 絶対パスが渡されることを検証する (実エディタは起動しない)。
func TestCmdOpenLaunchesEditorWithWorktree(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)

	// 実在する worktree ディレクトリ (絶対パス) を用意。
	wt := t.TempDir()
	// タスクファイル (worktree: に絶対パスを記録)。
	d := filepath.Join(store, "webapp")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "0001-x.md"),
		[]byte("---\nid: \"0001\"\nproject: webapp\ntitle: t\nstatus: in-progress\nworktree: "+wt+"\n---\n"), 0o644)

	// stub エディタ: 受け取った引数を marker に書く。
	bin := t.TempDir()
	marker := filepath.Join(bin, "opened.txt")
	os.WriteFile(filepath.Join(bin, "ed"), []byte("#!/bin/sh\necho \"$1\" > "+marker+"\n"), 0o755)
	t.Setenv("AGENT_TASKS_EDITOR", filepath.Join(bin, "ed"))

	if err := cmdOpen([]string{"webapp", "1"}); err != nil {
		t.Fatalf("cmdOpen: %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("stub エディタが起動されなかった: %v", err)
	}
	if strings.TrimSpace(string(got)) != wt {
		t.Errorf("エディタに渡った path = %q, want %q", strings.TrimSpace(string(got)), wt)
	}
}

// worktree が未記録のタスクはエラー。
func TestCmdOpenNoWorktree(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	d := filepath.Join(store, "webapp")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "0002-y.md"),
		[]byte("---\nid: \"0002\"\nproject: webapp\ntitle: t\nstatus: todo\n---\n"), 0o644)

	if err := cmdOpen([]string{"webapp", "2"}); err == nil {
		t.Error("worktree 未記録なのにエラーにならなかった")
	}
}

// worktree が存在しない (撤去済み) 場合はエラー。
func TestCmdOpenMissingWorktree(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	d := filepath.Join(store, "webapp")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "0003-z.md"),
		[]byte("---\nid: \"0003\"\nproject: webapp\ntitle: t\nstatus: done\nworktree: /nonexistent/path/xyz\n---\n"), 0o644)

	if err := cmdOpen([]string{"webapp", "3"}); err == nil {
		t.Error("worktree が存在しないのにエラーにならなかった")
	}
}
