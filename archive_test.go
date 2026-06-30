package main

import (
	"os"
	"path/filepath"
	"testing"
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
