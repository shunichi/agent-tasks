package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")

	// 新規作成 + perm。
	if err := atomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "first" {
		t.Errorf("content = %q, want first", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("perm = %v, want 0644", fi.Mode().Perm())
	}

	// 上書き。
	if err := atomicWriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("write2: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "second" {
		t.Errorf("content = %q, want second", b)
	}

	// 一時ファイルを残さない (rename 済み / 後始末済み)。
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "m.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir に余分なファイル: %v", names)
	}
}

func TestSessionStateFor(t *testing.T) {
	tests := []struct {
		event, notif, want string
	}{
		{"UserPromptSubmit", "", sessWorking},
		{"PreToolUse", "", sessWorking},
		{"PostToolUse", "", sessWorking},
		{"SessionStart", "", sessWorking},
		{"Stop", "", sessWaiting},
		{"Notification", "permission_prompt", sessWaiting},
		{"Notification", "idle_prompt", sessWaiting},
		{"Notification", "auth_success", ""}, // 状態に影響しない
		{"Notification", "", ""},
		{"SessionEnd", "", sessEnded},
		{"StopFailure", "", sessEnded},
		{"PreCompact", "", ""}, // 未対応イベントは無視
	}
	for _, tt := range tests {
		if got := sessionStateFor(tt.event, tt.notif); got != tt.want {
			t.Errorf("sessionStateFor(%q,%q) = %q, want %q", tt.event, tt.notif, got, tt.want)
		}
	}
}

func TestTaskSessionKey(t *testing.T) {
	tests := []struct {
		worktree, want string
	}{
		{"../agent-tasks--0020", "agent-tasks--0020"},
		{"/abs/path/family-app2--0001", "family-app2--0001"},
		{"", ""},
	}
	for _, tt := range tests {
		got := taskSessionKey(Task{Worktree: tt.worktree})
		if got != tt.want {
			t.Errorf("taskSessionKey(%q) = %q, want %q", tt.worktree, got, tt.want)
		}
	}
}

func TestWorktreeKey(t *testing.T) {
	// git 管理外は ""。
	nonGit := t.TempDir()
	if got := worktreeKey(nonGit); got != "" {
		t.Errorf("git 外で worktreeKey = %q, want \"\"", got)
	}

	// git repo の root basename を返す。
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Skipf("git init 不可のためスキップ: %v (%s)", err, out)
	}
	// git は realpath を返すので、比較側も symlink 解決した basename にそろえる。
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	want := filepath.Base(real)
	if got := worktreeKey(dir); got != want {
		t.Errorf("worktreeKey(%q) = %q, want %q", dir, got, want)
	}
}

func TestWriteReadSessionState(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	if _, ok := readSessionState("missing"); ok {
		t.Fatal("存在しないマーカーが ok=true")
	}
	if err := writeSessionState("agent-tasks--0020", sessWaiting, "/wt/agent-tasks--0020", now); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := readSessionState("agent-tasks--0020")
	if !ok {
		t.Fatal("書いたマーカーが読めない")
	}
	if got.State != sessWaiting {
		t.Errorf("state = %q, want %q", got.State, sessWaiting)
	}
	if got.Updated != "2026-06-29T12:00:00Z" {
		t.Errorf("updated = %q", got.Updated)
	}
	if got.Cwd != "/wt/agent-tasks--0020" {
		t.Errorf("cwd = %q", got.Cwd)
	}
	// 上書き
	if err := writeSessionState("agent-tasks--0020", sessWorking, "", now); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if got, _ := readSessionState("agent-tasks--0020"); got.State != sessWorking {
		t.Errorf("上書き後 state = %q, want %q", got.State, sessWorking)
	}
}

func TestWriteSessionStateRejectsBadKey(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"", "a/b", `a\b`} {
		if err := writeSessionState(key, sessWaiting, "", now); err == nil {
			t.Errorf("不正な key %q を受理した", key)
		}
	}
}

func TestSessionMarkerKey(t *testing.T) {
	if got := sessionMarkerKey("abc-123"); got != "sess-abc-123" {
		t.Errorf("sessionMarkerKey = %q", got)
	}
	if got := sessionMarkerKey(""); got != "" {
		t.Errorf("空 session_id で %q, want \"\"", got)
	}
}

func TestWriteReadSessionLink(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	if _, ok := readSessionLink("agent-tasks--0027"); ok {
		t.Fatal("存在しない link が ok=true")
	}
	if err := writeSessionLink("agent-tasks--0027", "sid-1", now); err != nil {
		t.Fatalf("write link: %v", err)
	}
	got, ok := readSessionLink("agent-tasks--0027")
	if !ok || got.SessionID != "sid-1" {
		t.Fatalf("read link = %+v ok=%v", got, ok)
	}
	// 不正キー/空 session_id は拒否。
	if err := writeSessionLink("a/b", "sid", now); err == nil {
		t.Error("不正な link key を受理した")
	}
	if err := writeSessionLink("k", "", now); err == nil {
		t.Error("空 session_id を受理した")
	}
}

// resolveSessionByCwd は cwd 一致の sess マーカーから最新の session_id を返す。
func TestResolveSessionByCwd(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	mk := func(id, cwd string, tm time.Time) {
		st := sessionState{State: sessWorking, Updated: tm.Format(time.RFC3339), Cwd: cwd, SessionID: id}
		if err := writeSessionMarker(sessionMarkerKey(id), st); err != nil {
			t.Fatalf("write sess marker: %v", err)
		}
	}
	base := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	mk("old", "/repo/main", base)
	mk("new", "/repo/main", base.Add(time.Minute)) // 同じ cwd で新しい方
	mk("other", "/repo/elsewhere", base.Add(time.Hour))

	if got := resolveSessionByCwd("/repo/main"); got != "new" {
		t.Errorf("resolveSessionByCwd(/repo/main) = %q, want new", got)
	}
	if got := resolveSessionByCwd("/nowhere"); got != "" {
		t.Errorf("一致なしで %q, want \"\"", got)
	}
}

// cmdSessionLink の --session 明示はパス区切りを弾く (cwd 逆引きより優先される経路の入力検証)。
func TestCmdSessionLinkRejectsBadSession(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	if err := os.MkdirAll(filepath.Join(store, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	task := "---\nid: \"0001\"\nproject: proj\nstatus: in-progress\nworktree: ../proj--0001\n---\n"
	if err := os.WriteFile(filepath.Join(store, "proj", "0001-x.md"), []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdSessionLink([]string{"proj", "0001", "--session", "a/b"}); err == nil {
		t.Error("不正な --session 値を受理した")
	}
	// 正常な --session は link を書く。
	if err := cmdSessionLink([]string{"proj", "0001", "--session", "sid-9"}); err != nil {
		t.Fatalf("正常 --session: %v", err)
	}
	if l, ok := readSessionLink("proj--0001"); !ok || l.SessionID != "sid-9" {
		t.Fatalf("link = %+v ok=%v, want sid-9", l, ok)
	}
}

// taskSessionState は worktree マーカーと link 経由 sess マーカーを突合し、新しい方を採る。
func TestTaskSessionState(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	task := Task{Status: "in-progress", Worktree: "../agent-tasks--0027"}
	key := "agent-tasks--0027"

	// どちらも無ければ ok=false。
	if _, ok := taskSessionState(task); ok {
		t.Fatal("マーカー無しで ok=true")
	}

	// worktree マーカーのみ (spawn 経路)。
	if err := writeSessionState(key, sessWorking, "/wt", now); err != nil {
		t.Fatal(err)
	}
	if st, ok := taskSessionState(task); !ok || st.State != sessWorking {
		t.Fatalf("worktree のみ: %+v ok=%v", st, ok)
	}

	// link 経由の sess マーカーが新しければそちらを採る (同一セッション start 経路)。
	if err := writeSessionMarker(sessionMarkerKey("sid"),
		sessionState{State: sessWaiting, Updated: now.Add(time.Minute).Format(time.RFC3339), SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink(key, "sid", now); err != nil {
		t.Fatal(err)
	}
	if st, ok := taskSessionState(task); !ok || st.State != sessWaiting {
		t.Fatalf("link が新しい: %+v ok=%v, want waiting", st, ok)
	}

	// worktree マーカーの方が新しければそちらを優先。
	if err := writeSessionState(key, sessEnded, "/wt", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, ok := taskSessionState(task); !ok || st.State != sessEnded {
		t.Fatalf("worktree が新しい: %+v ok=%v, want ended", st, ok)
	}
}

func TestMapHerdrStatus(t *testing.T) {
	cases := map[string]string{
		"working": sessWorking,
		"blocked": sessBlocked,
		"idle":    sessIdle,
		"unknown": "",
		"":        "",
	}
	for in, want := range cases {
		if got := mapHerdrStatus(in); got != want {
			t.Errorf("mapHerdrStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHerdrStateSnapshot(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	resetHerdrSnapshotCache()
	const js = `{"result":{"agents":[` +
		`{"agent":"claude","agent_status":"blocked","pane_id":"w3:p1","agent_session":{"value":"sid-a"}},` +
		`{"agent":"claude","agent_status":"working","pane_id":"w3:p2","agent_session":{"value":"sid-b"}}]}}`
	stubHerdrRun(t, []byte(js), nil)

	snap, ok := herdrStateSnapshot()
	if !ok {
		t.Fatal("herdr 有効なのに ok=false")
	}
	if snap["sid-a"] != "blocked" || snap["sid-b"] != "working" {
		t.Errorf("snapshot = %v", snap)
	}
}

func TestHerdrStateSnapshotDisabled(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	resetHerdrSnapshotCache()
	if _, ok := herdrStateSnapshot(); ok {
		t.Error("herdr 外では ok=false であるべき")
	}
}

// taskSessionState は herdr 経路 (link の session_id を agent_status に突合) を優先する。
func TestTaskSessionStateHerdr(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	task := Task{Status: "in-progress", Worktree: "../agent-tasks--0109"}
	key := "agent-tasks--0109"

	// link を張り、herdr にその session_id が blocked で存在 → blocked。
	if err := writeSessionLink(key, "sid-x", now); err != nil {
		t.Fatal(err)
	}
	resetHerdrSnapshotCache()
	stubHerdrRun(t, []byte(`{"result":{"agents":[{"agent_status":"blocked","pane_id":"w3:p1","agent_session":{"value":"sid-x"}}]}}`), nil)
	if st, ok := taskSessionState(task); !ok || st.State != sessBlocked {
		t.Fatalf("herdr blocked: %+v ok=%v", st, ok)
	}

	// link はあるが herdr に該当 agent 無し → ended。
	resetHerdrSnapshotCache()
	stubHerdrRun(t, []byte(`{"result":{"agents":[{"agent_status":"working","pane_id":"w3:p9","agent_session":{"value":"other"}}]}}`), nil)
	if st, ok := taskSessionState(task); !ok || st.State != sessEnded {
		t.Fatalf("herdr に無い→ended: %+v ok=%v", st, ok)
	}
}

// herdr 外ではマーカー経路にフォールバックする (既存挙動維持)。
func TestTaskSessionStateFallbackWhenNoHerdr(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_ENV", "0")
	resetHerdrSnapshotCache()
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	task := Task{Status: "in-progress", Worktree: "../agent-tasks--0109"}
	key := "agent-tasks--0109"
	if err := writeSessionState(key, sessWorking, "/wt", now); err != nil {
		t.Fatal(err)
	}
	if st, ok := taskSessionState(task); !ok || st.State != sessWorking {
		t.Fatalf("フォールバック: %+v ok=%v", st, ok)
	}
}

// TestPlanSessionPrune は掃除対象の判定を網羅する:
//   - 対応タスクが done / 存在しない worktree マーカー・link は対象。
//   - in-progress / blocked のタスクのマーカーは残す。
//   - sess マーカーは「生存 link から未参照 かつ retention 超」だけ対象 (参照中や新しいものは残す)。
//   - 壊れて読めない sess マーカーは十分古い扱い (未参照なら対象)。
//   - 上記に当たらないファイル (.tmp-*) は触らない。
func TestPlanSessionPrune(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	dir := sessionStateDir()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)   // retention(7d) 超
	young := now.Add(-1 * 24 * time.Hour) // retention 内
	mkSess := func(id string, tm time.Time) {
		st := sessionState{State: sessWorking, Updated: tm.Format(time.RFC3339), SessionID: id}
		if err := writeSessionMarker(sessionMarkerKey(id), st); err != nil {
			t.Fatal(err)
		}
	}

	// in-progress タスク: マーカー・link・(古い) sess マーカーすべて残る。
	if err := writeSessionState("proj--0001", sessWorking, "/wt", now); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0001", "sid-live", now); err != nil {
		t.Fatal(err)
	}
	mkSess("sid-live", old) // 古いが in-progress タスクに参照されるので残る

	// done タスク: worktree マーカー・link・未参照になった sess マーカーは対象。
	if err := writeSessionState("proj--0002", sessEnded, "/wt", now); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0002", "sid-done", now); err != nil {
		t.Fatal(err)
	}
	mkSess("sid-done", old)

	// 対応タスクの無い (存在しない) worktree マーカー・link は対象。
	if err := writeSessionState("proj--0099", sessWorking, "/wt", now); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0099", "sid-gone", now); err != nil {
		t.Fatal(err)
	}

	// blocked タスクのマーカーは残す (保留中を壊さない)。
	if err := writeSessionState("proj--0003", sessWaiting, "/wt", now); err != nil {
		t.Fatal(err)
	}

	mkSess("sid-young", young) // 未参照だが新しい → 残る
	// 壊れた sess マーカー (未参照) → 十分古い扱いで対象。
	if err := os.WriteFile(filepath.Join(dir, "sess-sid-broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 上記どれにも当たらないファイル → 触らない。
	if err := os.WriteFile(filepath.Join(dir, ".tmp-foo"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks := []Task{
		{Status: "in-progress", Worktree: "../proj--0001"},
		{Status: "done", Worktree: "../proj--0002"},
		{Status: "blocked", Worktree: "../proj--0003"},
	}
	got, err := planSessionPrune(tasks, now, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("planSessionPrune: %v", err)
	}
	var names []string
	for _, f := range got {
		names = append(names, f.Name)
	}
	want := []string{
		"proj--0002.json", "proj--0002.link.json",
		"proj--0099.json", "proj--0099.link.json",
		"sess-sid-broken.json", "sess-sid-done.json",
	}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("掃除対象 = %v\n           want %v", names, want)
	}
}

// TestPlanSessionPruneWorktime は worktime ログの掃除を確認する: 生存 link から参照される
// セッションのログは残し、未参照かつ retention 超のログだけ対象になる。
func TestPlanSessionPruneWorktime(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	young := now.Add(-1 * 24 * time.Hour)

	// in-progress タスク → sess-live を参照 (link 生存)。
	if err := writeSessionLink("proj--0001", "sess-live", now); err != nil {
		t.Fatal(err)
	}
	mklog := func(id string, mtime time.Time) {
		if err := appendWorktimeEvent(id, sessWorking, now); err != nil {
			t.Fatal(err)
		}
		p, _ := worktimeLogPath(id)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	mklog("sess-live", old)    // 参照されているので古くても残す
	mklog("sess-old", old)     // 未参照・古い → 対象
	mklog("sess-young", young) // 未参照だが新しい → 残す

	tasks := []Task{{Status: "in-progress", Worktree: "../proj--0001"}}
	got, err := planSessionPrune(tasks, now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range got {
		names = append(names, f.Name)
	}
	// worktime ログで対象になるのは sess-old のみ (proj--0001.link.json は生存タスクなので残る)。
	wantWorktime := filepath.Join("worktime", "sess-old.jsonl")
	if !slices.Contains(names, wantWorktime) {
		t.Errorf("sess-old の worktime ログが対象に無い: %v", names)
	}
	for _, unexpected := range []string{
		filepath.Join("worktime", "sess-live.jsonl"),
		filepath.Join("worktime", "sess-young.jsonl"),
	} {
		if slices.Contains(names, unexpected) {
			t.Errorf("残すべき worktime ログが対象になった: %s (%v)", unexpected, names)
		}
	}
}

// TestCmdSessionPrune はコマンド経路を検証する: --dry-run は消さず、既定実行は消す。
func TestCmdSessionPrune(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	dir := sessionStateDir()
	// done タスクを 1 件用意 (loadTasks が読む)。
	if err := os.MkdirAll(filepath.Join(store, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	task := "---\nid: \"0002\"\nproject: proj\nstatus: done\nworktree: ../proj--0002\n---\n"
	if err := os.WriteFile(filepath.Join(store, "proj", "0002-x.md"), []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// done タスクのマーカー (対象になる)。
	if err := writeSessionState("proj--0002", sessEnded, "/wt", now); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "proj--0002.json")

	// --dry-run: 消さない。
	if err := cmdSessionPrune([]string{"--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("--dry-run でマーカーが消えた: %v", err)
	}
	// 既定実行: 消す。
	if err := cmdSessionPrune(nil); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("実行後もマーカーが残っている (err=%v)", err)
	}
}

func TestValidWebSessionID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"session_01LU7EecqycqUbqHbhPMJ8gr", true},
		{"abc123", true},
		{"", false},
		{"has space", false},
		{"has/slash", false},
		{"has-dash", false}, // URL に素で埋め込むので英数字と _ のみ
	}
	for _, c := range cases {
		if got := validWebSessionID(c.in); got != c.want {
			t.Errorf("validWebSessionID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDetectSelfWebSessionURL(t *testing.T) {
	// env あり → claude.ai URL を組み立てる。
	t.Setenv("CLAUDE_CODE_BRIDGE_SESSION_ID", "session_01ABC")
	url, src := detectSelfWebSessionURL()
	if url != "https://claude.ai/code/session_01ABC" {
		t.Errorf("url = %q, want claude.ai/code/session_01ABC", url)
	}
	if src != "CLAUDE_CODE_BRIDGE_SESSION_ID" {
		t.Errorf("src = %q", src)
	}
	// env なし → 何も返さない (Remote Control 非接続 / 素の tmux)。
	t.Setenv("CLAUDE_CODE_BRIDGE_SESSION_ID", "")
	if url, _ := detectSelfWebSessionURL(); url != "" {
		t.Errorf("env 空なら空を返すべき, got %q", url)
	}
	// 不正な値 (URL に埋められない) → 弾く。
	t.Setenv("CLAUDE_CODE_BRIDGE_SESSION_ID", "bad/value")
	if url, _ := detectSelfWebSessionURL(); url != "" {
		t.Errorf("不正な id は弾くべき, got %q", url)
	}
}

func TestSetTaskSessionIfEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", dir)
	proj := filepath.Join(dir, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, "0001-x.md")
	src := "---\nid: \"0001\"\nproject: demo\ntitle: X\nstatus: in-progress\nsession:\n---\n\n# 要件\n本文はそのまま。\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// 空なので書き込む。
	set, err := setTaskSessionIfEmpty("demo", "0001", "https://claude.ai/code/session_01ABC")
	if err != nil {
		t.Fatal(err)
	}
	if !set {
		t.Fatal("空の session: には書き込むべき (set=false)")
	}
	pt, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if pt.Session != "https://claude.ai/code/session_01ABC" {
		t.Errorf("session = %q, want claude.ai URL", pt.Session)
	}
	// 本文が保全されている。
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "本文はそのまま。") {
		t.Errorf("本文が保全されていない:\n%s", b)
	}

	// 既に埋まっているので上書きしない (手動記録を尊重)。
	set, err = setTaskSessionIfEmpty("demo", "0001", "https://claude.ai/code/session_OTHER")
	if err != nil {
		t.Fatal(err)
	}
	if set {
		t.Error("既に埋まっているなら no-op であるべき (set=true)")
	}
	pt, _ = parseTask(path)
	if pt.Session != "https://claude.ai/code/session_01ABC" {
		t.Errorf("上書きされてしまった: session = %q", pt.Session)
	}
}
