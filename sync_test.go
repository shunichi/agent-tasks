package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initStoreRepo は temp に git リポジトリのストアを用意し、AGENT_TASKS_STORE を向ける。
func initStoreRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := git(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeStoreTask(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSyncScopedAddCommitsOnlyTarget は scoped sync (--path) が指定ファイルだけを
// コミットし、他セッションの dirty を巻き込まないことを確認する。
func TestSyncScopedAddCommitsOnlyTarget(t *testing.T) {
	dir := initStoreRepo(t)
	// ベースラインを 1 つコミットしておく (HEAD を作る)。
	writeStoreTask(t, dir, "agent-tasks/0001-a.md", "---\nid: \"0001\"\nstatus: todo\n---\n# a\n")
	if out, err := git(dir, "add", "-A"); err != nil {
		t.Fatalf("baseline add: %s", out)
	}
	if out, err := git(dir, "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("baseline commit: %s", out)
	}

	// 2 ファイルを dirty にする (0001 を変更、0002 を新規 = 別セッションの書きかけ相当)。
	writeStoreTask(t, dir, "agent-tasks/0001-a.md", "---\nid: \"0001\"\nstatus: in-progress\n---\n# a changed\n")
	writeStoreTask(t, dir, "agent-tasks/0002-b.md", "---\nid: \"0002\"\nstatus: todo\n---\n# b (別セッション)\n")

	if err := cmdSync([]string{"--no-push", "--path", "agent-tasks/0001-a.md"}); err != nil {
		t.Fatalf("cmdSync scoped: %v", err)
	}

	// HEAD のコミットは 0001 だけを含み、0002 は含まない。
	files, err := git(dir, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files, "0001-a.md") {
		t.Fatalf("0001 がコミットされていない: %q", files)
	}
	if strings.Contains(files, "0002-b.md") {
		t.Fatalf("0002 (別セッション) を巻き込んでコミットした: %q", files)
	}
	// 0002 は未追跡のまま残る。
	status, _ := git(dir, "status", "--porcelain")
	if !strings.Contains(status, "0002-b.md") {
		t.Fatalf("0002 が未コミットで残っていない: %q", status)
	}
}

// TestSyncNoArgStagesAll は引数なし sync が全体 (add -A) をコミットすることを確認する。
func TestSyncNoArgStagesAll(t *testing.T) {
	dir := initStoreRepo(t)
	writeStoreTask(t, dir, "agent-tasks/0001-a.md", "---\nid: \"0001\"\nstatus: todo\n---\n# a\n")
	writeStoreTask(t, dir, "agent-tasks/0002-b.md", "---\nid: \"0002\"\nstatus: todo\n---\n# b\n")

	if err := cmdSync([]string{"--no-push"}); err != nil {
		t.Fatalf("cmdSync 全体: %v", err)
	}
	files, err := git(dir, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0001-a.md", "0002-b.md"} {
		if !strings.Contains(files, want) {
			t.Fatalf("全体 sync で %s がコミットされていない: %q", want, files)
		}
	}
}

func TestIsNonFastForward(t *testing.T) {
	yes := []string{
		"! [rejected]        main -> main (non-fast-forward)",
		"Updates were rejected because the tip of your current branch is behind",
		"hint: Updates were rejected; ... fetch first",
	}
	for _, s := range yes {
		if !isNonFastForward(s) {
			t.Fatalf("non-fast-forward と判定すべき: %q", s)
		}
	}
	if isNonFastForward("Everything up-to-date") {
		t.Fatal("通常の成功出力を non-fast-forward と誤判定")
	}
}

func TestSyncLockPathStableAndOutsideStore(t *testing.T) {
	dir := t.TempDir()
	p1 := syncLockPath(dir)
	p2 := syncLockPath(dir)
	if p1 != p2 {
		t.Fatalf("同じストアで安定しない: %q != %q", p1, p2)
	}
	if strings.HasPrefix(p1, dir) {
		t.Fatalf("ロックがストア内に置かれている (add -A で混入する): %q", p1)
	}
	if p := syncLockPath(t.TempDir()); p == p1 {
		t.Fatal("別ストアで同じロックパスになっている")
	}
}

func TestLockFileExclusiveAndStale(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lockFile(lock, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("初回ロック取得失敗: %v", err)
	}
	// 生存中ロックは取得待ちでタイムアウトする。
	if _, err := lockFile(lock, 100*time.Millisecond, time.Minute); err == nil {
		t.Fatal("生存中ロックを二重取得できてしまった")
	}
	unlock()
	// 解放後は取得できる。
	unlock2, err := lockFile(lock, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("解放後にロック取得失敗: %v", err)
	}
	unlock2()
}
