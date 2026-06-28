package main

import (
	"strings"
	"testing"
	"time"
)

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m"},
		{2 * time.Hour, "2h"},
		{27*time.Hour + 10*time.Minute, "1d3h"},
		{48 * time.Hour, "2d"},
		{-5 * time.Minute, "0s"}, // 負値は 0 扱い
	}
	for _, tc := range cases {
		if got := humanizeDuration(tc.d); got != tc.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestLeadTime(t *testing.T) {
	if got := leadTime("2026-06-28T10:00:00+09:00", "2026-06-29T13:30:00+09:00"); got != "1d3h" {
		t.Errorf("leadTime = %q, want 1d3h", got)
	}
	// どちらか欠けると空。
	if got := leadTime("2026-06-28T10:00:00+09:00", ""); got != "" {
		t.Errorf("completed 欠落で空のはず, got %q", got)
	}
	if got := leadTime("", "2026-06-29T13:30:00+09:00"); got != "" {
		t.Errorf("started 欠落で空のはず, got %q", got)
	}
}

func TestTimestampSummary(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	c := colors{} // 無色

	// 記録なしは空 (footer を出さない)。
	if got := timestampSummary(Task{Status: "todo"}, now, c); got != "" {
		t.Errorf("記録なしは空のはず, got %q", got)
	}
	// 完了済みはリードタイムを含む。
	done := Task{Status: "done", StartedAt: "2026-06-28T10:00:00Z", CompletedAt: "2026-06-28T11:30:00Z"}
	if got := timestampSummary(done, now, c); got == "" ||
		!containsAll(got, "着手", "完了", "リードタイム", "1h30m") {
		t.Errorf("done summary = %q", got)
	}
	// 進行中は経過を含む。
	prog := Task{Status: "in-progress", StartedAt: "2026-06-29T10:00:00Z"}
	if got := timestampSummary(prog, now, c); !containsAll(got, "着手", "経過", "2h") {
		t.Errorf("in-progress summary = %q", got)
	}
}

func TestFindTimestampIssues(t *testing.T) {
	tasks := []Task{
		{Project: "p", ID: "0001", Status: "done", StartedAt: "2026-06-28T10:00:00Z", CompletedAt: "2026-06-28T12:00:00Z"}, // OK
		{Project: "p", ID: "0002", Status: "done", CompletedAt: "2026-06-28T12:00:00Z"},                                    // completed のみ
		{Project: "p", ID: "0003", Status: "done", StartedAt: "2026-06-29T10:00:00Z", CompletedAt: "2026-06-28T10:00:00Z"}, // 逆転
		{Project: "p", ID: "0004", Status: "done"},                                                                         // 旧 (どちらも無) → 無視
		{Project: "p", ID: "0005", Status: "in-progress", StartedAt: "2026-06-29T10:00:00Z"},                               // OK
		{Project: "p", ID: "0006", Status: "done", StartedAt: "2026-06-29T10:00:00Z"},                                      // done なのに completed 無し
	}
	issues := findTimestampIssues(tasks)
	if len(issues) != 3 {
		t.Fatalf("len(issues) = %d, want 3 (%+v)", len(issues), issues)
	}
	if issues[0].ID != "0002" || issues[1].ID != "0003" || issues[2].ID != "0006" {
		t.Errorf("検出対象がずれている: %+v", issues)
	}
}

func TestFindBlockedIssues(t *testing.T) {
	tasks := []Task{
		{Project: "p", ID: "0001", Status: "blocked", BlockedAt: "2026-06-28T10:00:00Z", BlockedReason: "確認待ち"}, // OK
		{Project: "p", ID: "0002", Status: "in-progress", BlockedAt: "2026-06-28T10:00:00Z"},                    // クリア漏れ
		{Project: "p", ID: "0003", Status: "done", BlockedReason: "残骸"},                                         // クリア漏れ
		{Project: "p", ID: "0004", Status: "blocked"},                                                           // blocked なのに blocked_at 無し
		{Project: "p", ID: "0005", Status: "todo"},                                                              // OK
	}
	issues := findBlockedIssues(tasks)
	if len(issues) != 3 {
		t.Fatalf("len(issues) = %d, want 3 (%+v)", len(issues), issues)
	}
	if issues[0].ID != "0002" || issues[1].ID != "0003" || issues[2].ID != "0004" {
		t.Errorf("検出対象がずれている: %+v", issues)
	}
}

// containsAll は s が部分文字列 subs を全て含むか。
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
