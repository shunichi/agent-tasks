package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBuildDashDataGroupsAndComputed(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Project: "alpha", Title: "T1", Status: "todo", Updated: "2026-07-01T00:00:00+09:00"},
		{ID: "0002", Project: "alpha", Title: "T2", Status: "blocked", BlockedAt: "2026-06-30T00:00:00+00:00", BlockedReason: "確認待ち", Updated: "u"},
		{ID: "0003", Project: "beta", Title: "T3", Status: "in-progress", Worktree: "../beta--0003", Updated: "u"},
	}
	d := buildDashData(rows, 5, now)

	if d.Count != 3 {
		t.Fatalf("Count = %d, want 3", d.Count)
	}
	if !d.Refresh || d.Interval != 5 {
		t.Errorf("Refresh/Interval = %v/%d, want true/5", d.Refresh, d.Interval)
	}
	if len(d.Groups) != 2 {
		t.Fatalf("Groups = %d, want 2 (alpha, beta)", len(d.Groups))
	}
	if d.Groups[0].Project != "alpha" || len(d.Groups[0].Rows) != 2 {
		t.Errorf("group0 = %q(%d rows), want alpha(2)", d.Groups[0].Project, len(d.Groups[0].Rows))
	}
	// blocked 行に経過と理由が入る。
	blocked := d.Groups[0].Rows[1]
	if blocked.BlockedFor == "" || blocked.BlockedReason != "確認待ち" {
		t.Errorf("blocked row = %+v, want BlockedFor 有 / reason 確認待ち", blocked)
	}
	// in-progress 行に session_state が入る (マーカー無し → unknown)。
	inprog := d.Groups[1].Rows[0]
	if inprog.SessionState != "unknown" {
		t.Errorf("in-progress session_state = %q, want unknown", inprog.SessionState)
	}
}

func TestBuildDashDataNoRefreshAndSessionURL(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Project: "alpha", Title: "T1", Status: "review",
			Session: "https://claude.ai/session_abc", PRs: []string{"https://x/pull/9"}, Updated: "u"},
		{ID: "0002", Project: "alpha", Title: "T2", Status: "todo", Session: "not-a-url", Updated: "u"},
	}
	d := buildDashData(rows, 0, now)
	if d.Refresh {
		t.Error("interval 0 では Refresh は false であるべき")
	}
	if got := d.Groups[0].Rows[0].SessionURL; got != "https://claude.ai/session_abc" {
		t.Errorf("SessionURL = %q, want claude.ai URL", got)
	}
	if got := d.Groups[0].Rows[1].SessionURL; got != "" {
		t.Errorf("http でない session はリンクしない, got %q", got)
	}
}

func TestRenderDashboardHTML(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Project: "alpha", Title: "a < b & c", Status: "todo", Updated: "2026-07-01T00:00:00+09:00"},
	}
	var buf bytes.Buffer
	if err := renderDashboard(&buf, rows, 5, now); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	// meta refresh が入る。
	if !strings.Contains(html, `http-equiv="refresh" content="5"`) {
		t.Error("meta refresh が無い")
	}
	// viewport (スマホ向け) が入る。
	if !strings.Contains(html, "width=device-width") {
		t.Error("viewport が無い (レスポンシブでない)")
	}
	// title は html/template により HTML エスケープされる (< → &lt;)。
	if !strings.Contains(html, "a &lt; b &amp; c") {
		t.Errorf("title がエスケープされていない: %s", html)
	}
	if strings.Contains(html, "a < b & c") {
		t.Error("生の < & が出力に残っている (XSS リスク)")
	}
}

func TestRenderDashboardEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDashboard(&buf, nil, 5, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "該当タスクなし") {
		t.Error("空一覧のプレースホルダが無い")
	}
}

func TestLanIPRankOrder(t *testing.T) {
	// 家庭内 LAN (192.168) が docker ブリッジ (172.x) より前に来る。
	if lanIPRank("192.168.11.2") >= lanIPRank("172.17.0.1") {
		t.Error("192.168 は 172.x より前であるべき")
	}
	if lanIPRank("10.0.2.1") >= lanIPRank("172.17.0.1") {
		t.Error("10.x は 172.x より前であるべき")
	}
	if lanIPRank("192.168.11.2") >= lanIPRank("10.0.2.1") {
		t.Error("192.168 は 10.x より前であるべき")
	}
}

func TestIsHTTPURL(t *testing.T) {
	for _, u := range []string{"http://x", "https://x/y"} {
		if !isHTTPURL(u) {
			t.Errorf("%q は URL とみなすべき", u)
		}
	}
	for _, u := range []string{"", "ftp://x", "session_abc", "//x"} {
		if isHTTPURL(u) {
			t.Errorf("%q は URL とみなすべきでない", u)
		}
	}
}
