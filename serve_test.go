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
		{"in-progress", "unknown", "in-progress"}, // マーカー未取得は working 扱いせず in-progress セクションへ
		{"in-progress", "ended", "in-progress"},   // セッション終了も in-progress セクション
		{"in-progress", "", "in-progress"},        // human 等 SESSION 無しの in-progress も in-progress
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
	// waiting → review → working → in-progress → other の固定順。unknown の in-progress は
	// other ではなく in-progress セクションへ、other には todo だけが残る。
	var gotKeys []string
	for _, g := range d.Groups {
		gotKeys = append(gotKeys, g.Key)
	}
	wantKeys := []string{"waiting", "review", "working", "in-progress", "other"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("group keys = %v, want %v", gotKeys, wantKeys)
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("group keys = %v, want %v", gotKeys, wantKeys)
		}
	}
	// in-progress セクションには 0005 (unknown in-progress) が 1 件。
	if got := d.Groups[3].Count; got != 1 {
		t.Errorf("in-progress セクションの件数 = %d, want 1", got)
	}
	// other には 0004 todo だけの 1 件。セクションの Count で見る。
	if got := d.Groups[4].Count; got != 1 {
		t.Errorf("other セクションの件数 = %d, want 1", got)
	}
	// 各セクションは project サブグループを持ち、カードは project を持つ。
	if len(d.Groups[0].Projects) != 1 || d.Groups[0].Projects[0].Project != "p" {
		t.Errorf("waiting セクションの project サブグループが不正: %+v", d.Groups[0].Projects)
	}
	if d.Groups[0].Projects[0].Rows[0].Project != "p" {
		t.Errorf("カードに project が無い: %+v", d.Groups[0].Projects[0].Rows[0])
	}
}

func TestBuildDashDataProjectSubgroups(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	// 同一セクション (review) 内に 2 project。project→id 順で 2 サブグループに分かれる。
	rows := []Task{
		{ID: "0001", Project: "alpha", Title: "A1", Status: "review", Updated: "u"},
		{ID: "0002", Project: "alpha", Title: "A2", Status: "review", Updated: "u"},
		{ID: "0003", Project: "beta", Title: "B1", Status: "review", Updated: "u"},
	}
	d := buildDashData(rows, 5, now)
	if len(d.Groups) != 1 || d.Groups[0].Key != "review" {
		t.Fatalf("Groups = %+v, want review 1 つ", d.Groups)
	}
	sec := d.Groups[0]
	if sec.Count != 3 {
		t.Errorf("review セクションの Count = %d, want 3", sec.Count)
	}
	if len(sec.Projects) != 2 {
		t.Fatalf("project サブグループ = %d, want 2 (alpha, beta)", len(sec.Projects))
	}
	if sec.Projects[0].Project != "alpha" || len(sec.Projects[0].Rows) != 2 {
		t.Errorf("subgroup0 = %q(%d), want alpha(2)", sec.Projects[0].Project, len(sec.Projects[0].Rows))
	}
	if sec.Projects[1].Project != "beta" || len(sec.Projects[1].Rows) != 1 {
		t.Errorf("subgroup1 = %q(%d), want beta(1)", sec.Projects[1].Project, len(sec.Projects[1].Rows))
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
			Session: "https://claude.ai/code/session_abc", PRs: []string{"https://x/pull/9"}, Updated: "u"},
		{ID: "0002", Project: "alpha", Title: "T2", Status: "todo", Session: "not-a-url", Updated: "u"},
	}
	d := buildDashData(rows, 0, now)
	if d.Refresh {
		t.Error("interval 0 では Refresh は false であるべき")
	}
	// review セクション (先) の行は http URL なので web リンク + アプリリンク両方を持つ。
	review := d.Groups[0].Projects[0].Rows[0]
	if review.SessionURL != "https://claude.ai/code/session_abc" {
		t.Errorf("SessionURL = %q, want claude.ai/code URL", review.SessionURL)
	}
	if string(review.SessionAppURL) != "claude://code/session_abc" {
		t.Errorf("SessionAppURL = %q, want claude://code/session_abc", review.SessionAppURL)
	}
	// other セクション (後) の todo は http でない session なのでどちらもリンクしない。
	other := d.Groups[1].Projects[0].Rows[0]
	if other.SessionURL != "" || other.SessionAppURL != "" {
		t.Errorf("http でない session はリンクしない, got web=%q app=%q", other.SessionURL, other.SessionAppURL)
	}
}

func TestClaudeAppURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://claude.ai/code/session_01ABC", "claude://code/session_01ABC"},
		{"http://claude.ai/code/session_01ABC", "claude://code/session_01ABC"},
		{"https://claude.ai/code/", ""},            // session id 無し
		{"https://claude.ai/session_abc", ""},      // /code/ 配下でない
		{"https://example.com/code/session_x", ""}, // 別ホスト
		{"", ""},
	}
	for _, c := range cases {
		if got := claudeAppURL(c.in); got != c.want {
			t.Errorf("claudeAppURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderDashboardAppLinkNotSanitized(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Project: "alpha", Title: "T", Status: "review",
			Session: "https://claude.ai/code/session_01ABC", Updated: "u"},
	}
	var buf bytes.Buffer
	if err := renderDashboard(&buf, rows, 5, now); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	// claude:// スキームが html/template の URL サニタイズで潰されず残る (template.URL 経由)。
	if !strings.Contains(html, `href="claude://code/session_01ABC"`) {
		t.Errorf("claude:// アプリリンクが出力されていない (サニタイズされた?): %s", html)
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Error("URL がサニタイズされている (#ZgotmplZ が出た)")
	}
	// web (https) フォールバックも併記される。
	if !strings.Contains(html, `href="https://claude.ai/code/session_01ABC"`) {
		t.Error("web フォールバックリンクが無い")
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
