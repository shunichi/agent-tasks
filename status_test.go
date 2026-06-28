package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFormatSyncStatus(t *testing.T) {
	var c colors // 色なし (空フィールド) でテキストだけ検証する
	cases := []struct {
		name string
		st   SyncStatus
		want string
	}{
		{"not git", SyncStatus{NotGit: true}, "git 管理されていません (git init とリモート設定が必要)"},
		{"clean", SyncStatus{Upstream: "origin/main"}, "クリーン (同期済み) — origin/main"},
		{"dirty", SyncStatus{Dirty: 3, Upstream: "origin/main"}, "未コミット 3 ファイル (origin/main)"},
		{"no upstream", SyncStatus{NoUpstream: true}, "upstream 未設定 (未 push)"},
		{"ahead+behind", SyncStatus{Ahead: 2, Behind: 1, Upstream: "origin/main"},
			"未 push 2 コミット / 未取得 1 コミット (origin/main)"},
		{"dirty+ahead", SyncStatus{Dirty: 1, Ahead: 2, Upstream: "origin/main"},
			"未コミット 1 ファイル / 未 push 2 コミット (origin/main)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSyncStatus(tc.st, c); got != tc.want {
				t.Errorf("formatSyncStatus()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestSyncStatusClean(t *testing.T) {
	cases := []struct {
		st   SyncStatus
		want bool
	}{
		{SyncStatus{Upstream: "origin/main"}, true},
		{SyncStatus{NotGit: true}, false},
		{SyncStatus{NoUpstream: true}, false},
		{SyncStatus{Dirty: 1, Upstream: "origin/main"}, false},
		{SyncStatus{Ahead: 1, Upstream: "origin/main"}, false},
		{SyncStatus{Behind: 1, Upstream: "origin/main"}, false},
	}
	for _, tc := range cases {
		if got := tc.st.Clean(); got != tc.want {
			t.Errorf("Clean(%+v) = %v, want %v", tc.st, got, tc.want)
		}
	}
}

func TestLoadSyncStatusNotGit(t *testing.T) {
	st, err := loadSyncStatus(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !st.NotGit {
		t.Errorf("plain dir: NotGit = false, want true")
	}
}

// TestLoadSyncStatusWithUpstream は bare remote を立て、clone→commit→push の各段階で
// dirty / ahead / behind が正しく数えられるかを通しで確認する。
func TestLoadSyncStatusWithUpstream(t *testing.T) {
	bare := t.TempDir()
	mustGit(t, bare, "init", "-q", "--bare")

	clone := t.TempDir()
	mustGit(t, clone, "clone", "-q", bare, ".")
	mustGit(t, clone, "config", "user.email", "t@example.com")
	mustGit(t, clone, "config", "user.name", "t")
	writeFile(t, filepath.Join(clone, "a.md"), "a\n")
	mustGit(t, clone, "add", "-A")
	mustGit(t, clone, "commit", "-q", "-m", "init")
	// push して upstream を確立 (ブランチ名は環境差を避けて現在ブランチを使う)
	branch, err := git(clone, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, clone, "push", "-q", "-u", "origin", branch)

	// 1. クリーン
	st, err := loadSyncStatus(clone)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Clean() {
		t.Errorf("after push: not clean: %+v", st)
	}

	// 2. 未コミット (working tree を汚す)
	writeFile(t, filepath.Join(clone, "a.md"), "a changed\n")
	writeFile(t, filepath.Join(clone, "b.md"), "b\n")
	st, _ = loadSyncStatus(clone)
	if st.Dirty != 2 {
		t.Errorf("dirty = %d, want 2", st.Dirty)
	}

	// 3. コミットすると dirty=0 / ahead=1
	mustGit(t, clone, "add", "-A")
	mustGit(t, clone, "commit", "-q", "-m", "more")
	st, _ = loadSyncStatus(clone)
	if st.Dirty != 0 || st.Ahead != 1 || st.Behind != 0 {
		t.Errorf("after commit: dirty=%d ahead=%d behind=%d, want 0/1/0", st.Dirty, st.Ahead, st.Behind)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
