package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// makeStore は temp ストアに project ディレクトリを作り、AGENT_TASKS_STORE を向ける。
func makeStore(t *testing.T, project string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, project), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TASKS_STORE", dir)
	return dir
}

func writeStoreFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadTasksMarksEmptyReservationIncomplete は 0 バイトの予約ファイルが Incomplete と
// 印付けされ、中身のあるタスクは印付けされないことを確認する。
func TestLoadTasksMarksEmptyReservationIncomplete(t *testing.T) {
	dir := makeStore(t, "webapp")
	proj := filepath.Join(dir, "webapp")
	writeStoreFile(t, filepath.Join(proj, "0001-a.md"), "---\nid: \"0001\"\nstatus: todo\ntitle: A\n---\n")
	writeStoreFile(t, filepath.Join(proj, "0002-reserved.md"), "") // alloc-id の空予約相当

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 空予約は frontmatter が無く ID はファイル名全体になるので、Path で同定する。
	byBase := map[string]Task{}
	for _, t := range tasks {
		byBase[filepath.Base(t.Path)] = t
	}
	if byBase["0001-a.md"].Incomplete {
		t.Error("中身のあるタスク 0001 が Incomplete 扱いになっている")
	}
	if !byBase["0002-reserved.md"].Incomplete {
		t.Error("空予約 0002-reserved が Incomplete と印付けされていない")
	}
}

// TestSelectTasksHidesIncomplete は一覧 (selectTasks) が空予約を除外することを確認する。
func TestSelectTasksHidesIncomplete(t *testing.T) {
	dir := makeStore(t, "webapp")
	proj := filepath.Join(dir, "webapp")
	writeStoreFile(t, filepath.Join(proj, "0001-a.md"), "---\nid: \"0001\"\nstatus: todo\ntitle: A\n---\n")
	writeStoreFile(t, filepath.Join(proj, "0002-reserved.md"), "")

	// allProjects=true で cwd の project 判定に依存させない。
	rows, _, _, err := selectTasks("", nil, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if !slices.Contains(ids, "0001") {
		t.Fatalf("正常タスク 0001 が一覧に出ていない: %v", ids)
	}
	if slices.Contains(ids, "0002-reserved") {
		t.Fatalf("空予約が一覧に出てしまった: %v", ids)
	}
}
