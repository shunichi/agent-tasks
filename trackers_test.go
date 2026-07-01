package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tracker: はブロックリスト形式 (prs: と同様) でパースされ、複数持てる。
func TestParseTrackerBlockList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-x.md")
	content := "---\n" +
		"id: \"0001\"\n" +
		"title: foo\n" +
		"tracker:\n" +
		"  - https://example.com/issues/1\n" +
		"  - https://tracker.example.org/t/2\n" +
		"---\n本文\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tracker) != 2 ||
		got.Tracker[0] != "https://example.com/issues/1" ||
		got.Tracker[1] != "https://tracker.example.org/t/2" {
		t.Fatalf("Tracker = %v, want 2 件", got.Tracker)
	}
}

// 1 行カンマ区切りも許容する (prs: と同じ)。
func TestParseTrackerInline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0002-x.md")
	content := "---\nid: \"0002\"\ntracker: https://a.example/1, https://b.example/2\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := parseTask(path)
	if len(got.Tracker) != 2 {
		t.Fatalf("Tracker = %v, want 2 件 (カンマ区切り)", got.Tracker)
	}
}

// prs: と tracker: が同居しても混ざらない (別フィールド)。
func TestParsePRsAndTrackerSeparate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0003-x.md")
	content := "---\n" +
		"id: \"0003\"\n" +
		"prs:\n  - https://example.com/pr/1\n" +
		"tracker:\n  - https://example.com/issue/9\n" +
		"---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := parseTask(path)
	if len(got.PRs) != 1 || got.PRs[0] != "https://example.com/pr/1" {
		t.Errorf("PRs = %v, want 1 件のみ", got.PRs)
	}
	if len(got.Tracker) != 1 || got.Tracker[0] != "https://example.com/issue/9" {
		t.Errorf("Tracker = %v, want 1 件のみ", got.Tracker)
	}
}

func TestFindTrackerProblems(t *testing.T) {
	tasks := []Task{
		{Project: "p", ID: "0001", Tracker: []string{"https://example.com/1"}},
		{Project: "p", ID: "0002", Tracker: []string{"not-a-url"}},
		{Project: "p", ID: "0003"},
	}
	probs := findTrackerProblems(tasks)
	if len(probs) != 1 || probs[0].ID != "0002" {
		t.Fatalf("probs = %+v, want 1 件 (0002)", probs)
	}
}

func TestTrackerSummary(t *testing.T) {
	c := colors{}
	if s := trackerSummary(Task{}, c); s != "" {
		t.Errorf("空タスクのサマリは空であるべき: %q", s)
	}
	s := trackerSummary(Task{Tracker: []string{"https://example.com/1"}}, c)
	if s == "" || !strings.Contains(s, "https://example.com/1") {
		t.Errorf("サマリに URL が含まれない: %q", s)
	}
}
