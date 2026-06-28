package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestResolveListScope(t *testing.T) {
	cases := []struct {
		name        string
		filterProj  string
		allProjects bool
		current     string
		wantProj    string
		wantCross   bool
	}{
		{"既定は現在 project", "", false, "family-app2", "family-app2", false},
		{"--all-projects で横断", "", true, "family-app2", "", true},
		{"--project 明示は別 project でも従う", "other", false, "family-app2", "other", false},
		{"--project は --all-projects より優先", "other", true, "family-app2", "other", false},
		{"git 外は横断にフォールバック", "", false, "", "", true},
		{"git 外でも --project は効く", "other", false, "", "other", false},
	}
	for _, tc := range cases {
		gotProj, gotCross := resolveListScope(tc.filterProj, tc.allProjects, tc.current)
		if gotProj != tc.wantProj || gotCross != tc.wantCross {
			t.Errorf("%s: resolveListScope(%q, %v, %q) = (%q, %v), want (%q, %v)",
				tc.name, tc.filterProj, tc.allProjects, tc.current,
				gotProj, gotCross, tc.wantProj, tc.wantCross)
		}
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

func TestLeadingID(t *testing.T) {
	cases := map[string]string{
		"0005-store-git-sync.md": "0005",
		"12-foo.md":              "12",
		"0001.md":                "0001",
		"README.md":              "", // 数字始まりでない
		"abc.md":                 "",
	}
	for in, want := range cases {
		if got := leadingID(in); got != want {
			t.Errorf("leadingID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSyncCommitMessage(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "0005-sync.md"),
		[]byte("---\nid: \"0005\"\nstatus: in-progress\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 単一の変更 -> 件名にステータス込みで1件。
	single := syncCommitMessage(dir, "M\tdemo/0005-sync.md")
	if single != "tasks: demo/0005 (in-progress)" {
		t.Errorf("single = %q", single)
	}

	// 削除はファイルを読まず removed 扱い。
	del := syncCommitMessage(dir, "D\tdemo/0009-old.md")
	if del != "tasks: demo/0009 (removed)" {
		t.Errorf("deleted = %q", del)
	}

	// 複数 -> 件数を件名に、本文に列挙。タスク以外 (README) は無視。
	multi := syncCommitMessage(dir, "M\tdemo/0005-sync.md\nD\tdemo/0009-old.md\nM\tREADME.md")
	if !strings.HasPrefix(multi, "tasks: update 2 tasks\n\n- demo/0005 (in-progress)\n- demo/0009 (removed)") {
		t.Errorf("multi = %q", multi)
	}

	// タスクファイルが無い変更のみ -> 汎用メッセージ。
	if got := syncCommitMessage(dir, "M\tREADME.md"); got != "tasks: sync store" {
		t.Errorf("generic = %q", got)
	}
}
