package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// liveSessionID は task の link session_id が snap (herdr の session_id 集合) に居れば返す。
func TestLiveSessionID(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}
	task := Task{Project: "proj", ID: "0001", Worktree: "../proj--0001"}

	// snap に居る → その sid。
	snap := map[string]string{"sid-A": "working", "sid-other": "idle"}
	if got := liveSessionID(task, snap); got != "sid-A" {
		t.Errorf("live: got %q, want sid-A", got)
	}
	// snap に居ない → ""。
	if got := liveSessionID(task, map[string]string{"sid-other": "idle"}); got != "" {
		t.Errorf("not live: got %q, want \"\"", got)
	}
	// status が空文字 (unknown) でもキーが在れば居るとみなす。
	if got := liveSessionID(task, map[string]string{"sid-A": ""}); got != "sid-A" {
		t.Errorf("unknown status but present: got %q, want sid-A", got)
	}
	// link 無し → ""。
	noLink := Task{Project: "proj", ID: "0002", Worktree: "../proj--0002"}
	if got := liveSessionID(noLink, snap); got != "" {
		t.Errorf("no link: got %q, want \"\"", got)
	}
	// worktree 無し → ""。
	if got := liveSessionID(Task{Project: "proj", ID: "0003"}, snap); got != "" {
		t.Errorf("no worktree: got %q, want \"\"", got)
	}
}

// refreshLiveTasks は herdr にライブな link を持つタスクだけを liveKeys に入れる。
func TestRefreshLiveTasks(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	// 0001=ライブ (sid-A), 0002=ended (link あるが herdr に無い), 0003=link 無し。
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0002", "sid-gone", now); err != nil {
		t.Fatal(err)
	}
	m := &tuiModel{all: []Task{
		{Project: "proj", ID: "0001", Worktree: "../proj--0001"},
		{Project: "proj", ID: "0002", Worktree: "../proj--0002"},
		{Project: "proj", ID: "0003", Worktree: "../proj--0003"},
	}}

	resetHerdrSnapshotCache()
	stubHerdrRun(t, []byte(`{"result":{"agents":[{"agent_status":"working","pane_id":"w3:p1","agent_session":{"value":"sid-A"}}]}}`), nil)
	m.refreshLiveTasks()

	if !m.liveKeys["proj/0001"] {
		t.Error("0001 はライブのはず")
	}
	if m.liveKeys["proj/0002"] {
		t.Error("0002 は ended なのでライブでないはず")
	}
	if m.liveKeys["proj/0003"] {
		t.Error("0003 は link 無しなのでライブでないはず")
	}
	if len(m.liveKeys) != 1 {
		t.Errorf("liveKeys の件数 = %d, want 1", len(m.liveKeys))
	}
}

// herdr 外では liveKeys は空 (印なし = degrade)。
func TestRefreshLiveTasksNoHerdr(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_ENV", "0")
	resetHerdrSnapshotCache()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}
	m := &tuiModel{all: []Task{{Project: "proj", ID: "0001", Worktree: "../proj--0001"}}}
	m.refreshLiveTasks()
	if len(m.liveKeys) != 0 {
		t.Errorf("herdr 外では liveKeys は空のはず: %v", m.liveKeys)
	}
}

// liveColWidth / visibleLiveCount は可視行 (rows) のライブ有無で決まる。
func TestLiveColAndCount(t *testing.T) {
	m := &tuiModel{
		liveKeys: map[string]bool{"proj/0001": true, "proj/0009": true}, // 0009 は rows に無い
		rows: []Task{
			{Project: "proj", ID: "0001"},
			{Project: "proj", ID: "0002"},
		},
	}
	if got := m.liveColWidth(); got != tuiLiveColW {
		t.Errorf("liveColWidth = %d, want %d (可視行にライブ有り)", got, tuiLiveColW)
	}
	if got := m.visibleLiveCount(); got != 1 {
		t.Errorf("visibleLiveCount = %d, want 1 (0009 は不可視)", got)
	}

	// 可視行にライブが無ければ列は出さない。
	m2 := &tuiModel{
		liveKeys: map[string]bool{"proj/0009": true},
		rows:     []Task{{Project: "proj", ID: "0001"}},
	}
	if got := m2.liveColWidth(); got != 0 {
		t.Errorf("liveColWidth = %d, want 0 (可視行にライブ無し)", got)
	}
	if got := m2.visibleLiveCount(); got != 0 {
		t.Errorf("visibleLiveCount = %d, want 0", got)
	}

	// liveKeys 空なら常に 0。
	m3 := &tuiModel{rows: []Task{{Project: "proj", ID: "0001"}}}
	if got := m3.liveColWidth(); got != 0 {
		t.Errorf("liveColWidth (空) = %d, want 0", got)
	}
}

// listCols は liveW を fixed に足す (ライブ列が出るとき title 前の固定幅が広がる)。
func TestListColsIncludesLive(t *testing.T) {
	rows := []Task{{Project: "proj", ID: "0001", Status: "todo"}}
	base := &tuiModel{effProjects: []string{"proj"}, rows: rows}
	_, _, _, liveW0, fixed0 := base.listCols()
	if liveW0 != 0 {
		t.Fatalf("ライブ無しで liveW=%d, want 0", liveW0)
	}

	live := &tuiModel{effProjects: []string{"proj"}, rows: rows, liveKeys: map[string]bool{"proj/0001": true}}
	_, _, _, liveW1, fixed1 := live.listCols()
	if liveW1 != tuiLiveColW {
		t.Fatalf("ライブ有りで liveW=%d, want %d", liveW1, tuiLiveColW)
	}
	if fixed1 != fixed0+tuiLiveColW+1 {
		t.Errorf("fixed がライブ列ぶん広がっていない: %d → %d (want +%d)", fixed0, fixed1, tuiLiveColW+1)
	}
}

// renderList / renderHeader はライブなタスクに緑 ● とヘッダの ●ライブ:N を出す。
func TestRenderLiveMarker(t *testing.T) {
	tasks := []Task{
		{Project: "alpha", ID: "0001", Status: "in-progress", Title: "live task"},
		{Project: "alpha", ID: "0002", Status: "todo", Title: "quiet task"},
	}
	m := &tuiModel{all: tasks, effProjects: []string{"alpha"}, liveKeys: map[string]bool{"alpha/0001": true}}
	m.applyFilter()
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := model.(*tuiModel)

	list := mm.renderList()
	if !strings.Contains(list, "●") {
		t.Errorf("ライブ行に ● が出ていない:\n%s", list)
	}
	if !strings.Contains(mm.renderHeader(), "●ライブ:1") {
		t.Errorf("ヘッダに ●ライブ:1 が出ていない:\n%s", mm.renderHeader())
	}

	// ライブが無ければ ● 列もヘッダ表記も出ない。
	m2 := &tuiModel{all: tasks, effProjects: []string{"alpha"}}
	m2.applyFilter()
	var model2 tea.Model = m2
	model2, _ = model2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm2 := model2.(*tuiModel)
	if strings.Contains(mm2.renderList(), "●") {
		t.Errorf("ライブ無しなのに ● が出ている:\n%s", mm2.renderList())
	}
	if strings.Contains(mm2.renderHeader(), "●ライブ") {
		t.Errorf("ライブ無しなのにヘッダに ●ライブ が出ている:\n%s", mm2.renderHeader())
	}
}

// tuiSessionLabel は herdr の blocked/idle も返す (従来 working/waiting/ended のみだった)。
func TestTuiSessionLabelHerdrStates(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	task := Task{Status: "in-progress", Worktree: "../proj--0001"}
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ status, want string }{
		{"blocked", "blocked"},
		{"idle", "idle"},
		{"working", "working"},
	} {
		resetHerdrSnapshotCache()
		js := `{"result":{"agents":[{"agent_status":"` + tc.status + `","pane_id":"w3:p1","agent_session":{"value":"sid-A"}}]}}`
		stubHerdrRun(t, []byte(js), nil)
		if got := tuiSessionLabel(task); got != tc.want {
			t.Errorf("agent_status=%s → label %q, want %q", tc.status, got, tc.want)
		}
	}
}
