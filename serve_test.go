package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTaskSectionClassify(t *testing.T) {
	cases := []struct {
		status, sess, want string
	}{
		{"in-progress", sessWaiting, "waiting"},
		{"in-progress", sessWorking, "working"},
		{"in-progress", "unknown", "other"}, // マーカー未取得は working 扱いしない
		{"in-progress", "ended", "other"},
		{"review", "", "review"},
		{"todo", "", "other"},
		{"blocked", "", "other"},
		{"done", "", "other"},
	}
	for _, c := range cases {
		if got := taskSection(c.status, c.sess); got != c.want {
			t.Errorf("taskSection(%q,%q) = %q, want %q", c.status, c.sess, got, c.want)
		}
	}
}

func TestBuildDashDataStateSectionsOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_TASKS_STATE_DIR", dir)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	// SESSION マーカーを用意して waiting/working を作る (worktree basename がキー)。
	if err := writeSessionState("p--0001", sessWaiting, "", now); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionState("p--0003", sessWorking, "", now); err != nil {
		t.Fatal(err)
	}
	rows := []Task{
		{ID: "0001", Project: "p", Title: "待ち", Status: "in-progress", Worktree: "../p--0001", Updated: "u"},
		{ID: "0002", Project: "p", Title: "レビュー", Status: "review", Updated: "u"},
		{ID: "0003", Project: "p", Title: "実行中", Status: "in-progress", Worktree: "../p--0003", Updated: "u"},
		{ID: "0004", Project: "p", Title: "todo", Status: "todo", Updated: "u"},
		{ID: "0005", Project: "p", Title: "マーカー無 in-progress", Status: "in-progress", Worktree: "../p--0005", Updated: "u"},
	}
	d := buildDashData(rows, 5, now)

	if d.Count != 5 {
		t.Fatalf("Count = %d, want 5", d.Count)
	}
	// waiting → review → working → other の固定順。other には todo と unknown の in-progress が入る。
	var gotKeys []string
	for _, g := range d.Groups {
		gotKeys = append(gotKeys, g.Key)
	}
	wantKeys := []string{"waiting", "review", "working", "other"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("group keys = %v, want %v", gotKeys, wantKeys)
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("group keys = %v, want %v", gotKeys, wantKeys)
		}
	}
	// other には 2 件 (0004 todo, 0005 unknown in-progress)。
	if got := len(d.Groups[3].Rows); got != 2 {
		t.Errorf("other セクションの件数 = %d, want 2", got)
	}
	// カードは project を持つ。
	if d.Groups[0].Rows[0].Project != "p" {
		t.Errorf("カードに project が無い: %+v", d.Groups[0].Rows[0])
	}
}

func TestBuildDashDataEmptySectionsSkipped(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	// review だけ → review セクション 1 つだけ (空の waiting/working/other は出さない)。
	rows := []Task{{ID: "0001", Project: "p", Title: "R", Status: "review", Updated: "u"}}
	d := buildDashData(rows, 5, now)
	if len(d.Groups) != 1 || d.Groups[0].Key != "review" {
		t.Fatalf("Groups = %+v, want review 1 つだけ", d.Groups)
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
	// review セクション (先) の行は http URL なのでリンクする。
	if got := d.Groups[0].Rows[0].SessionURL; got != "https://claude.ai/session_abc" {
		t.Errorf("SessionURL = %q, want claude.ai URL", got)
	}
	// other セクション (後) の todo は http でない session なのでリンクしない。
	if got := d.Groups[1].Rows[0].SessionURL; got != "" {
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
