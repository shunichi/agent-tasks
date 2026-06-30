package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestUnderRepo(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		rel  string
		want bool
	}{
		{"a", true},
		{"a/b/c", true},
		{".env", true},
		{"", false},
		{"..", false},
		{".." + sep + "etc" + sep + "passwd", false}, // worktree の外へ脱出
		{"/etc/passwd", false},                       // 絶対パス
	}
	for _, tt := range tests {
		if got := underRepo(tt.rel); got != tt.want {
			t.Errorf("underRepo(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

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

// .worktreeinclude に書いた対象が symlink のときは追従せずコピーしない。
// (リンク先が repo 外を指し得るため。Lstat で判定する。)
func TestCopyWorktreeIncludesSkipsSymlink(t *testing.T) {
	main := initRepo(t)
	// repo 外の機微ファイルを用意し、repo 内 symlink がそれを指す。
	outside := filepath.Join(t.TempDir(), "outside-secret")
	writeFile(t, outside, "OUTSIDE")
	if err := os.Symlink(outside, filepath.Join(main, "link-to-outside")); err != nil {
		t.Fatal(err)
	}
	// ディレクトリ配下の symlink も追従しないことを確認する。
	writeFile(t, filepath.Join(main, "dir", "real.txt"), "REAL")
	if err := os.Symlink(outside, filepath.Join(main, "dir", "link.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(main, ".gitignore"), "link-to-outside\ndir/\n.env\n")
	writeFile(t, filepath.Join(main, ".worktreeinclude"), "link-to-outside\ndir\n.env\n")
	writeFile(t, filepath.Join(main, ".env"), "SECRET=1")

	wt := t.TempDir()
	copied, err := copyWorktreeIncludes(main, wt)
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(copied)
	want := []string{".env", filepath.Join("dir", "real.txt")}
	if !slices.Equal(copied, want) {
		t.Fatalf("copied = %v, want %v (symlink は除外されるべき)", copied, want)
	}
	// symlink 自体もリンク先の中身も worktree に持ち込まれない。
	if _, err := os.Lstat(filepath.Join(wt, "link-to-outside")); err == nil {
		t.Error("repo 外を指す symlink がコピーされてしまった")
	}
	if _, err := os.Lstat(filepath.Join(wt, "dir", "link.txt")); err == nil {
		t.Error("ディレクトリ配下の symlink がコピーされてしまった")
	}
}

// dst に壊れた (dangling) symlink がある場合、Lstat で「在る」と判定してスキップし、
// symlink を追従して target を新規作成しない。
func TestCopyWorktreeIncludesSkipsDanglingSymlinkDst(t *testing.T) {
	main := initRepo(t)
	writeFile(t, filepath.Join(main, ".gitignore"), ".env\n")
	writeFile(t, filepath.Join(main, ".worktreeinclude"), ".env\n")
	writeFile(t, filepath.Join(main, ".env"), "FROM_MAIN")

	wt := t.TempDir()
	// dst を存在しない先への symlink にする (dangling)。
	target := filepath.Join(t.TempDir(), "nonexistent")
	if err := os.Symlink(target, filepath.Join(wt, ".env")); err != nil {
		t.Fatal(err)
	}

	copied, err := copyWorktreeIncludes(main, wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 0 {
		t.Errorf("dangling symlink の dst はスキップされるべき: copied = %v", copied)
	}
	// symlink を追従して target を作っていないこと。
	if _, err := os.Stat(target); err == nil {
		t.Error("dst の symlink を追従して target を作ってしまった")
	}
}

// copyFile は単体でも symlink を追従しない。
func TestCopyFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	writeFile(t, real, "DATA")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(link, filepath.Join(dir, "dst")); err == nil {
		t.Error("copyFile は symlink を拒否すべき")
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
