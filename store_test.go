package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-foo.md")
	content := `---
id: "0001"
project: family-app2
title: ブックマークのドラッグ並び替え
status: in-progress
agent: claude
---

# 要件
本文
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "0001" {
		t.Errorf("ID = %q, want 0001", got.ID)
	}
	if got.Project != "family-app2" {
		t.Errorf("Project = %q, want family-app2", got.Project)
	}
	if got.Title != "ブックマークのドラッグ並び替え" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Status != "in-progress" {
		t.Errorf("Status = %q, want in-progress", got.Status)
	}
}

func TestParseTaskNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte("# just markdown\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" || got.Title != "" {
		t.Errorf("expected empty task, got %+v", got)
	}
}

func TestLoadTasksSorted(t *testing.T) {
	dir := t.TempDir()
	write := func(proj, name, body string) {
		d := filepath.Join(dir, proj)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, name), []byte(body), 0o644)
	}
	write("zeta", "0002-b.md", "---\nid: \"0002\"\nstatus: todo\n---\n")
	write("zeta", "0001-a.md", "---\nid: \"0001\"\nstatus: todo\n---\n")
	write("alpha", "0001-a.md", "---\nid: \"0001\"\nstatus: done\n---\n")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore me"), 0o644)

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("len = %d, want 3 (README.md must be ignored)", len(tasks))
	}
	// project 昇順 -> id 昇順
	if tasks[0].Project != "alpha" || tasks[1].Project != "zeta" || tasks[1].ID != "0001" {
		t.Errorf("sort order wrong: %+v", tasks)
	}
}

func TestDispWidth(t *testing.T) {
	if got := dispWidth("abc"); got != 3 {
		t.Errorf("dispWidth(abc) = %d, want 3", got)
	}
	if got := dispWidth("あ"); got != 2 {
		t.Errorf("dispWidth(あ) = %d, want 2", got)
	}
	if got := dispWidth("aあ"); got != 3 {
		t.Errorf("dispWidth(aあ) = %d, want 3", got)
	}
}
