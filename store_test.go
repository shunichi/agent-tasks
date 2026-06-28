package main

import (
	"os"
	"path/filepath"
	"slices"
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

func TestEditorArgv(t *testing.T) {
	// 既定は code。
	t.Setenv("AGENT_TASKS_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := editorArgv(); len(got) != 1 || got[0] != "code" {
		t.Errorf("default = %v, want [code]", got)
	}

	// 優先順位 AGENT_TASKS_EDITOR > VISUAL > EDITOR、かつ引数も分割する。
	t.Setenv("EDITOR", "vi")
	t.Setenv("VISUAL", "nano")
	t.Setenv("AGENT_TASKS_EDITOR", "code -w")
	if got := editorArgv(); !slices.Equal(got, []string{"code", "-w"}) {
		t.Errorf("got %v, want [code -w]", got)
	}

	t.Setenv("AGENT_TASKS_EDITOR", "")
	if got := editorArgv(); !slices.Equal(got, []string{"nano"}) {
		t.Errorf("VISUAL precedence: got %v, want [nano]", got)
	}
}

func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"5":     "0005", // 短縮形を4桁ゼロ埋めに
		"05":    "0005", // 中途半端なゼロ埋めも揃える
		"0005":  "0005", // 既存形式はそのまま
		"0":     "0000",
		"12345": "12345", // 4桁を超えても切り詰めない
		"foo":   "foo",   // 非数値はそのまま
		"":      "",      // 空も素通し
		"-5":    "-5",    // 負数は照合しない (そのまま返す)
	}
	for in, want := range cases {
		if got := normalizeID(in); got != want {
			t.Errorf("normalizeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveTaskPathShortID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", dir)
	proj := filepath.Join(dir, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(proj, "0005-flexible.md")
	if err := os.WriteFile(want, []byte("---\nid: \"0005\"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"5", "05", "0005"} {
		got, err := resolveTaskPath("demo", id)
		if err != nil {
			t.Errorf("resolveTaskPath(demo, %q) error: %v", id, err)
			continue
		}
		if got != want {
			t.Errorf("resolveTaskPath(demo, %q) = %q, want %q", id, got, want)
		}
	}

	if _, err := resolveTaskPath("demo", "9"); err == nil {
		t.Error("存在しない id 9 はエラーになるべき")
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
