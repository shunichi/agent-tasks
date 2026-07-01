package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTask はテスト用に <store>/<proj>[/<sub>]/<name> へ最小タスクを書く。
func writeTask(t *testing.T, store, proj, sub, name, body string) string {
	t.Helper()
	d := filepath.Join(store, proj)
	if sub != "" {
		d = filepath.Join(d, sub)
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// アクティブ走査 (loadTasks) はアーカイブを読まず、loadArchivedTasks だけが読む。
func TestArchiveScanSeparation(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-active.md", "---\nid: \"0001\"\nstatus: todo\n---\n")
	writeTask(t, store, "webapp", archiveDirName, "0002-old.md", "---\nid: \"0002\"\nstatus: done\n---\n")

	active, err := loadTasks(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "0001" || active[0].Archived {
		t.Fatalf("active = %+v, want only 0001 (Archived=false)", active)
	}

	arch, _, err := loadArchivedTasks(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(arch) != 1 || arch[0].ID != "0002" || !arch[0].Archived {
		t.Fatalf("archived = %+v, want only 0002 (Archived=true)", arch)
	}
}

// 採番はアーカイブの最大値も算入する (退避済みの番号を再利用しない)。
func TestMaxTaskIDIncludesArchive(t *testing.T) {
	store := t.TempDir()
	projDir := filepath.Join(store, "webapp")
	writeTask(t, store, "webapp", "", "0001-active.md", "")
	writeTask(t, store, "webapp", archiveDirName, "0009-old.md", "")

	if got := maxTaskID(projDir); got != 9 {
		t.Errorf("maxTaskID = %d, want 9 (アーカイブの 0009 を算入)", got)
	}
}

// cmdArchive はファイルを archive/ へ移し、アクティブ走査から消える。unarchive で戻る。
func TestCmdArchiveRoundTrip(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	writeTask(t, store, "webapp", "", "0005-foo.md", "---\nid: \"0005\"\nstatus: todo\ntitle: foo\n---\n")

	if err := cmdArchive([]string{"webapp", "5"}); err != nil {
		t.Fatalf("cmdArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", "0005-foo.md")); !os.IsNotExist(err) {
		t.Errorf("アクティブにファイルが残っている: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", archiveDirName, "0005-foo.md")); err != nil {
		t.Errorf("archive/ に移動していない: %v", err)
	}
	if active, _ := loadTasks(store); len(active) != 0 {
		t.Errorf("アーカイブ後もアクティブ一覧に出る: %+v", active)
	}

	if err := cmdUnarchive([]string{"webapp", "5"}); err != nil {
		t.Fatalf("cmdUnarchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", "0005-foo.md")); err != nil {
		t.Errorf("unarchive でアクティブに戻っていない: %v", err)
	}
}

// 既にアーカイブ済みの id を archive するとエラー (取り違え防止)。
func TestCmdArchiveAlreadyArchived(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	writeTask(t, store, "webapp", archiveDirName, "0003-x.md", "---\nid: \"0003\"\n---\n")

	if err := cmdArchive([]string{"webapp", "3"}); err == nil {
		t.Error("既にアーカイブ済みなのにエラーにならなかった")
	}
}

// unarchive の戻し先にアクティブな同 id があれば衝突エラー (番号再利用の保険)。
func TestCmdUnarchiveCollision(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	writeTask(t, store, "webapp", "", "0007-active.md", "---\nid: \"0007\"\n---\n")
	writeTask(t, store, "webapp", archiveDirName, "0007-old.md", "---\nid: \"0007\"\n---\n")

	if err := cmdUnarchive([]string{"webapp", "7"}); err == nil {
		t.Error("戻し先にアクティブな同 id があるのに衝突エラーにならなかった")
	}
}

// autoArchiveTargets は done かつ completed_at が閾値より古いものだけを選ぶ。
// review / in-progress / completed_at 無し / 期間内は対象外。
func TestAutoArchiveTargets(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Status: "done", CompletedAt: "2026-05-01T00:00:00Z"},   // 61 日前 → 対象
		{ID: "0002", Status: "done", CompletedAt: "2026-06-25T00:00:00Z"},   // 6 日前 → 対象外
		{ID: "0003", Status: "done", CompletedAt: "2026-06-01T12:00:00Z"},   // ちょうど 30 日前 → 対象 (>=)
		{ID: "0004", Status: "done", CompletedAt: ""},                       // completed_at 無し → 対象外
		{ID: "0005", Status: "review", CompletedAt: "2026-01-01T00:00:00Z"}, // review → 対象外
		{ID: "0006", Status: "in-progress", CompletedAt: ""},                // → 対象外
	}
	got := autoArchiveTargets(rows, 30*24*time.Hour, now)
	if len(got) != 2 {
		t.Fatalf("対象 %d 件, want 2 (0001, 0003): %+v", len(got), got)
	}
	if got[0].ID != "0001" || got[1].ID != "0003" {
		t.Errorf("対象 = %s, %s; want 0001, 0003", got[0].ID, got[1].ID)
	}
}

// cmdAutoArchive は完了後 N 日を過ぎた done だけを archive/ へ移し、
// 最近完了 / review / completed_at 無しは残す。
func TestCmdAutoArchive(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	old := "2020-01-01T00:00:00+09:00"
	recent := time.Now().Format(time.RFC3339)
	writeTask(t, store, "webapp", "", "0001-old.md",
		"---\nid: \"0001\"\nproject: webapp\nstatus: done\ntitle: old\ncompleted_at: \""+old+"\"\n---\n")
	writeTask(t, store, "webapp", "", "0002-recent.md",
		"---\nid: \"0002\"\nproject: webapp\nstatus: done\ntitle: recent\ncompleted_at: \""+recent+"\"\n---\n")
	writeTask(t, store, "webapp", "", "0003-review.md",
		"---\nid: \"0003\"\nproject: webapp\nstatus: review\ntitle: review\ncompleted_at: \""+old+"\"\n---\n")
	writeTask(t, store, "webapp", "", "0004-nodate.md",
		"---\nid: \"0004\"\nproject: webapp\nstatus: done\ntitle: no date\n---\n")

	if err := cmdAutoArchive([]string{"--project", "webapp", "--older-than", "30"}); err != nil {
		t.Fatalf("cmdAutoArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", archiveDirName, "0001-old.md")); err != nil {
		t.Errorf("0001 (完了後 N 日超) が archive されていない: %v", err)
	}
	for _, n := range []string{"0002-recent.md", "0003-review.md", "0004-nodate.md"} {
		if _, err := os.Stat(filepath.Join(store, "webapp", n)); err != nil {
			t.Errorf("%s が誤って移動された (対象外のはず): %v", n, err)
		}
	}
}

// --dry-run は対象を表示するだけでファイルを動かさない。
func TestCmdAutoArchiveDryRun(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	writeTask(t, store, "webapp", "", "0001-old.md",
		"---\nid: \"0001\"\nproject: webapp\nstatus: done\ntitle: old\ncompleted_at: \"2020-01-01T00:00:00+09:00\"\n---\n")
	if err := cmdAutoArchive([]string{"--project", "webapp", "--dry-run"}); err != nil {
		t.Fatalf("cmdAutoArchive --dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", "0001-old.md")); err != nil {
		t.Errorf("--dry-run なのにアクティブから消えた: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "webapp", archiveDirName, "0001-old.md")); !os.IsNotExist(err) {
		t.Errorf("--dry-run なのに archive/ へ移動された")
	}
}

// doctor の重複検査はアクティブ + アーカイブを横断する (同 id がまたがると検出)。
func TestDoctorDupSpansArchive(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0004-a.md", "---\nid: \"0004\"\n---\n")
	writeTask(t, store, "webapp", archiveDirName, "0004-b.md", "---\nid: \"0004\"\n---\n")

	active, _, _ := loadTasksReport(store)
	archived, _, _ := loadArchivedTasks(store)
	dups := findDuplicateIDs(append(active, archived...))
	if len(dups) != 1 || dups[0].ID != "0004" || len(dups[0].Paths) != 2 {
		t.Fatalf("dups = %+v, want 1 件で 0004 が active/archive にまたがる", dups)
	}
}
