package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchQueryTitle(t *testing.T) {
	task := Task{Title: "ブックマークのドラッグ並び替え"}
	if !matchQuery(task, "", false) {
		t.Error("空クエリは常に一致するべき")
	}
	if !matchQuery(task, "ドラッグ", false) {
		t.Error("タイトル部分一致にヒットするべき")
	}
	if !matchQuery(Task{Title: "Fix CI Flake"}, "ci", false) {
		t.Error("大文字小文字を区別しないべき")
	}
	if matchQuery(task, "存在しない語", false) {
		t.Error("非一致は false")
	}
}

// content=false のときは本文にしか無い語はヒットしない。content=true でヒットする。
func TestMatchQueryContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-x.md")
	os.WriteFile(path, []byte("---\nid: \"0001\"\ntitle: 短いタイトル\n---\n# 要件\n本文だけにある秘密ワード\n"), 0o644)
	task := Task{Title: "短いタイトル", Path: path}

	if matchQuery(task, "秘密ワード", false) {
		t.Error("content=false で本文語にヒットしてはいけない")
	}
	if !matchQuery(task, "秘密ワード", true) {
		t.Error("content=true で本文語にヒットするべき")
	}
	// タイトル語は content 設定に関係なくヒット。
	if !matchQuery(task, "タイトル", false) {
		t.Error("タイトル語は常にヒット")
	}
}

// selectTasks に query を渡すとタイトル一致だけに絞られる。
func TestSelectTasksSearch(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	write := func(id, title string) {
		d := filepath.Join(store, "webapp")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, id+"-x.md"),
			[]byte("---\nid: \""+id+"\"\nproject: webapp\ntitle: "+title+"\nstatus: todo\n---\n本文\n"), 0o644)
	}
	write("0001", "検索対象のタスク")
	write("0002", "別のもの")

	rows, _, _, err := selectTasks("", []string{"webapp"}, false, false, false, "検索対象", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "0001" {
		t.Fatalf("検索結果 = %+v, want 0001 のみ", rows)
	}
}
