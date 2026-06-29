package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseTaskPRsBlockList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-foo.md")
	// prs: の後にブロックリスト、さらに別キーが続く境界も確認する。
	content := `---
id: "0001"
title: PR テスト
status: review
prs:
  - https://github.com/shunichi/agent-tasks/pull/31
  - "https://github.com/shunichi/agent-tasks/pull/33"
updated: "2026-06-29T13:00:00+09:00"
---

# 本文
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://github.com/shunichi/agent-tasks/pull/31",
		"https://github.com/shunichi/agent-tasks/pull/33",
	}
	if !slices.Equal(got.PRs, want) {
		t.Errorf("PRs = %v, want %v", got.PRs, want)
	}
	// リスト終端後のキーが正しく読めている (listKey のリセット確認)。
	if got.Updated != "2026-06-29T13:00:00+09:00" {
		t.Errorf("Updated = %q (prs リストが後続キーを飲み込んだ可能性)", got.Updated)
	}
}

func TestParseTaskPRsInlineComma(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0002-bar.md")
	content := `---
id: "0002"
title: inline
prs: https://x/pull/1, https://x/pull/2
---
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://x/pull/1", "https://x/pull/2"}
	if !slices.Equal(got.PRs, want) {
		t.Errorf("PRs = %v, want %v", got.PRs, want)
	}
}

func TestParseTaskNoPRs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0003-baz.md")
	content := "---\nid: \"0003\"\ntitle: none\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PRs) != 0 {
		t.Errorf("PRs = %v, want empty", got.PRs)
	}
}

func TestPRSummary(t *testing.T) {
	// PR 無しは空。
	if s := prSummary(Task{}, colors{}); s != "" {
		t.Errorf("empty PRs should yield no footer, got %q", s)
	}
	// PR ありは各 URL が 1 行ずつ含まれる。
	tk := Task{PRs: []string{"https://x/pull/1", "https://x/pull/2"}}
	s := prSummary(tk, colors{})
	if !strings.Contains(s, "https://x/pull/1") || !strings.Contains(s, "https://x/pull/2") {
		t.Errorf("prSummary missing URLs: %q", s)
	}
	if strings.Count(s, "\n") != 2 { // "PR:" 行 + 2 項目 = 改行 2 個
		t.Errorf("prSummary line count unexpected: %q", s)
	}
}

func TestFindPRIssues(t *testing.T) {
	tasks := []Task{
		{Project: "p", ID: "0001", PRs: []string{"https://github.com/x/pull/1"}}, // OK
		{Project: "p", ID: "0002", PRs: []string{"not-a-url"}},                   // NG
		{Project: "p", ID: "0003", PRs: []string{"https://ok/pull/9", "ftp://bad"}},
	}
	got := findPRIssues(tasks)
	if len(got) != 2 {
		t.Fatalf("findPRIssues = %d issues, want 2: %+v", len(got), got)
	}
	if got[0].ID != "0002" || got[1].ID != "0003" {
		t.Errorf("unexpected issues: %+v", got)
	}
}
