package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskBodyStripsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-x.md")
	content := "---\nid: \"0001\"\ntitle: foo\n---\n\n# 要件\n本文です\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := taskBody(path)
	if strings.Contains(got, "id:") || strings.Contains(got, "---") {
		t.Errorf("frontmatter が残っている: %q", got)
	}
	if !strings.HasPrefix(got, "# 要件") {
		t.Errorf("本文の先頭がずれている: %q", got)
	}
	if !strings.Contains(got, "本文です") {
		t.Errorf("本文が欠けている: %q", got)
	}
}

func TestTaskBodyNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	os.WriteFile(path, []byte("# just markdown\nbody\n"), 0o644)
	got := taskBody(path)
	if !strings.Contains(got, "just markdown") {
		t.Errorf("frontmatter 無しで本文が取れない: %q", got)
	}
}

// setFrontmatterFields は既存キーを置換し、無いキーは閉じ --- の前に挿入する。
// 値に : を含む URL/日時はダブルクォートで囲み、parseTask が読み戻せる。
func TestSetFrontmatterFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-x.md")
	content := "---\nid: \"0001\"\ntitle: foo\nstatus: todo\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n\n# 要件\n本文\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	url := "https://github.com/owner/repo/issues/12"
	now := "2026-07-01T08:14:56+09:00"
	if err := setFrontmatterFields(path, []fmField{{"issue", url}, {"updated", now}}); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Issue != url {
		t.Errorf("issue = %q, want %q", got.Issue, url)
	}
	if got.Updated != now {
		t.Errorf("updated = %q, want %q (既存キーを置換)", got.Updated, now)
	}
	if got.ID != "0001" || got.Title != "foo" || got.Status != "todo" {
		t.Errorf("他フィールドが壊れた: %+v", got)
	}
	// 本文が保持されていること。
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# 要件") || !strings.Contains(string(raw), "本文") {
		t.Errorf("本文が壊れた:\n%s", raw)
	}
	// URL はクォートされていること (: を含むため)。
	if !strings.Contains(string(raw), "issue: \""+url+"\"") {
		t.Errorf("issue 値がクォートされていない:\n%s", raw)
	}
}

func TestSetFrontmatterFieldsNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	os.WriteFile(path, []byte("# no frontmatter\n"), 0o644)
	if err := setFrontmatterFields(path, []fmField{{"issue", "https://x/1"}}); err == nil {
		t.Error("frontmatter が無いのにエラーにならなかった")
	}
}

func TestQuoteFrontmatterValue(t *testing.T) {
	cases := map[string]string{
		"plain":                           "plain",
		"https://github.com/o/r/issues/1": "\"https://github.com/o/r/issues/1\"",
		"has#hash":                        "\"has#hash\"",
		"":                                "\"\"",
	}
	for in, want := range cases {
		if got := quoteFrontmatterValue(in); got != want {
			t.Errorf("quoteFrontmatterValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoBase(t *testing.T) {
	cases := map[string]string{
		"owner/repo":            "repo",
		"github.com/owner/repo": "repo",
		"repo":                  "repo",
	}
	for in, want := range cases {
		if got := repoBase(in); got != want {
			t.Errorf("repoBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastURL(t *testing.T) {
	out := "Creating issue in owner/repo\nhttps://github.com/owner/repo/issues/42"
	if got := lastURL(out); got != "https://github.com/owner/repo/issues/42" {
		t.Errorf("lastURL = %q", got)
	}
	if got := lastURL("no url here"); got != "" {
		t.Errorf("lastURL(no url) = %q, want empty", got)
	}
}

func TestFindIssueProblems(t *testing.T) {
	tasks := []Task{
		{Project: "p", ID: "0001", Issue: "https://github.com/o/r/issues/1"},
		{Project: "p", ID: "0002", Issue: "owner/repo#3"}, // URL ではない
		{Project: "p", ID: "0003", Issue: ""},             // 無しは対象外
	}
	probs := findIssueProblems(tasks)
	if len(probs) != 1 || probs[0].ID != "0002" {
		t.Fatalf("probs = %+v, want 1 件 (0002)", probs)
	}
}
