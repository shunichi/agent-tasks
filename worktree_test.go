package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// initRepo は temp に git リポジトリを作り、root を返す (テスト用)。
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCopyWorktreeIncludes(t *testing.T) {
	main := initRepo(t)
	// gitignore 対象: .env, .env.local, config/secrets.json。tracked: app.txt。
	writeFile(t, filepath.Join(main, ".gitignore"), ".env\n.env.local\nconfig/secrets.json\n")
	writeFile(t, filepath.Join(main, ".worktreeinclude"), "# env をコピー\n.env\n.env.local\nconfig/secrets.json\napp.txt\n")
	writeFile(t, filepath.Join(main, ".env"), "SECRET=1")
	writeFile(t, filepath.Join(main, ".env.local"), "LOCAL=2")
	writeFile(t, filepath.Join(main, "config", "secrets.json"), "{}")
	writeFile(t, filepath.Join(main, "app.txt"), "tracked")

	wt := t.TempDir()
	copied, err := copyWorktreeIncludes(main, wt)
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(copied)
	want := []string{".env", ".env.local", filepath.Join("config", "secrets.json")}
	if !slices.Equal(copied, want) {
		t.Fatalf("copied = %v, want %v (tracked app.txt は除外されるべき)", copied, want)
	}
	// 中身がコピーされていること。
	if b, _ := os.ReadFile(filepath.Join(wt, ".env")); string(b) != "SECRET=1" {
		t.Errorf(".env の内容が違う: %q", b)
	}
	// tracked ファイルは複製されない。
	if _, err := os.Stat(filepath.Join(wt, "app.txt")); err == nil {
		t.Error("tracked な app.txt がコピーされてしまった")
	}
}

func TestCopyWorktreeIncludesSkipsExisting(t *testing.T) {
	main := initRepo(t)
	writeFile(t, filepath.Join(main, ".gitignore"), ".env\n")
	writeFile(t, filepath.Join(main, ".worktreeinclude"), ".env\n")
	writeFile(t, filepath.Join(main, ".env"), "FROM_MAIN")

	wt := t.TempDir()
	writeFile(t, filepath.Join(wt, ".env"), "ALREADY_HERE")

	copied, err := copyWorktreeIncludes(main, wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 0 {
		t.Errorf("既存ファイルはスキップされるべき: copied = %v", copied)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, ".env")); string(b) != "ALREADY_HERE" {
		t.Errorf("既存 .env が上書きされた: %q", b)
	}
}

func TestCopyWorktreeIncludesNoFile(t *testing.T) {
	main := initRepo(t)
	copied, err := copyWorktreeIncludes(main, t.TempDir())
	if err != nil {
		t.Fatalf(".worktreeinclude が無いときはエラーにせず no-op: %v", err)
	}
	if copied != nil {
		t.Errorf("copied = %v, want nil", copied)
	}
}

func TestRunPostCreate(t *testing.T) {
	main := initRepo(t)
	// worktree を実際に作る (マーカーが worktree 固有 git dir に置かれるため)。
	wt := filepath.Join(filepath.Dir(main), filepath.Base(main)+"--wt")
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-q", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("git", "-C", main, "worktree", "remove", "--force", wt).Run() })

	// post-create は cwd と env を確認できるよう成果物を書き出す。
	hook := filepath.Join(main, ".worktree-post-create")
	writeFile(t, hook, "#!/bin/sh\necho \"$AGENT_TASKS_PROJECT\" > ran.txt\npwd >> ran.txt\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}

	ran, err := runPostCreate(main, wt, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("post-create が実行されなかった")
	}
	out, err := os.ReadFile(filepath.Join(wt, "ran.txt"))
	if err != nil {
		t.Fatalf("成果物が無い: %v", err)
	}
	if want := filepath.Base(main); !slices.Contains([]string{want}, firstLine(string(out))) {
		t.Errorf("AGENT_TASKS_PROJECT = %q, want %q", firstLine(string(out)), want)
	}

	// 2 回目はマーカーでスキップされる。
	ran2, err := runPostCreate(main, wt, false)
	if err != nil {
		t.Fatal(err)
	}
	if ran2 {
		t.Error("2 回目はマーカーでスキップされるべき")
	}

	// --force で再実行される。
	ran3, err := runPostCreate(main, wt, true)
	if err != nil {
		t.Fatal(err)
	}
	if !ran3 {
		t.Error("--force で再実行されるべき")
	}
}

func TestRunPostCreateNoHook(t *testing.T) {
	main := initRepo(t)
	ran, err := runPostCreate(main, t.TempDir(), false)
	if err != nil {
		t.Fatalf("フックが無いときはエラーにせず no-op: %v", err)
	}
	if ran {
		t.Error("ran = true, want false")
	}
}
