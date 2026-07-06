package main

import (
	"strings"
	"testing"
)

// stubResolveSession は resolveSessionIDForPane をテスト用に差し替え、後始末する。
func stubResolveSession(t *testing.T, fn func(pane string) (string, error)) {
	t.Helper()
	orig := resolveSessionIDForPane
	resolveSessionIDForPane = fn
	t.Cleanup(func() { resolveSessionIDForPane = orig })
}

// TestWorktimeRecordParsesNestedEvent はイベント JSON (data 配下ネスト) を読み、pane から
// 解決した session_id の worktime ログに agent_status をそのまま追記することを確認する。
func TestWorktimeRecordParsesNestedEvent(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	stubResolveSession(t, func(pane string) (string, error) {
		if pane != "w3:p8" {
			t.Fatalf("pane = %q, want w3:p8", pane)
		}
		return "sess-abc", nil
	})
	t.Setenv("HERDR_PLUGIN_EVENT_JSON",
		`{"event":"pane_agent_status_changed","data":{"type":"pane_agent_status_changed","pane_id":"w3:p8","workspace_id":"w3","agent_status":"working","agent":"claude"}}`)

	if err := cmdWorktimeRecord(nil); err != nil {
		t.Fatalf("cmdWorktimeRecord: %v", err)
	}
	evs, err := readWorktimeEvents("sess-abc")
	if err != nil {
		t.Fatalf("readWorktimeEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].State != "working" {
		t.Fatalf("events = %+v, want 1 件 working", evs)
	}
}

// TestWorktimeRecordDedupsConsecutiveSameState は直近と同じ状態の再送を記録しないことを確認する
// (herdr 再起動時の再送などで重複しても区間集計を汚さない)。
func TestWorktimeRecordDedupsConsecutiveSameState(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	stubResolveSession(t, func(pane string) (string, error) { return "sess-x", nil })

	record := func(status string) {
		t.Setenv("HERDR_PLUGIN_EVENT_JSON",
			`{"event":"pane_agent_status_changed","data":{"pane_id":"w3:p1","agent_status":"`+status+`"}}`)
		if err := cmdWorktimeRecord(nil); err != nil {
			t.Fatalf("cmdWorktimeRecord(%s): %v", status, err)
		}
	}
	record("working")
	record("working") // 重複: 記録されない
	record("idle")
	record("working")

	evs, err := readWorktimeEvents("sess-x")
	if err != nil {
		t.Fatalf("readWorktimeEvents: %v", err)
	}
	got := make([]string, len(evs))
	for i, e := range evs {
		got[i] = e.State
	}
	want := []string{"working", "idle", "working"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

// TestWorktimeRecordNoSession は pane に agent セッションが無いとき何も記録しないことを確認する。
func TestWorktimeRecordNoSession(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	stubResolveSession(t, func(pane string) (string, error) { return "", nil }) // セッション未確立
	t.Setenv("HERDR_PLUGIN_EVENT_JSON",
		`{"event":"pane_agent_status_changed","data":{"pane_id":"w3:p1","agent_status":"working"}}`)
	if err := cmdWorktimeRecord(nil); err != nil {
		t.Fatalf("cmdWorktimeRecord: %v", err)
	}
	if evs, _ := readWorktimeEvents(""); len(evs) != 0 {
		t.Fatalf("記録されないはず: %+v", evs)
	}
}

// TestWorktimeRecordBadInput は空/不正 JSON でも失敗せず (exit 0) no-op になることを確認する
// (プラグイン hook は herdr のセッションを乱さない方針)。
func TestWorktimeRecordBadInput(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	stubResolveSession(t, func(pane string) (string, error) {
		t.Fatal("解決まで到達しないはず")
		return "", nil
	})
	// 空
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", "")
	if err := cmdWorktimeRecord(nil); err != nil {
		t.Fatalf("空入力でエラー: %v", err)
	}
	// 壊れた JSON
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", "{not json")
	if err := cmdWorktimeRecord(nil); err != nil {
		t.Fatalf("不正 JSON でエラー: %v", err)
	}
	// agent_status 欠落 (状態不明) → 記録しない
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", `{"data":{"pane_id":"w3:p1"}}`)
	if err := cmdWorktimeRecord(nil); err != nil {
		t.Fatalf("status 欠落でエラー: %v", err)
	}
}

// TestReadWorktimeEventsStableOrder は ts が同値のイベント (同一瞬間に追記) が追記順を保つことを
// 確認する (安定ソート)。非安定だと working→blocked→working の順が壊れ、区間復元/dedup を誤る。
func TestReadWorktimeEventsStableOrder(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	sid := "sess-sameinstant"
	now := tm("10:00") // 全イベントを同一瞬間で追記 → ts が同値になる
	for _, st := range []string{sessWorking, sessBlocked, sessWorking} {
		if err := appendWorktimeEvent(sid, st, now); err != nil {
			t.Fatalf("appendWorktimeEvent(%s): %v", st, err)
		}
	}
	evs, err := readWorktimeEvents(sid)
	if err != nil {
		t.Fatalf("readWorktimeEvents: %v", err)
	}
	got := make([]string, len(evs))
	for i, e := range evs {
		got[i] = e.State
	}
	want := []string{sessWorking, sessBlocked, sessWorking}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("順序 = %v, want %v (安定ソートで追記順を保つべき)", got, want)
	}
}

// TestWorktimeRecordPrintPlugin は --print-plugin が event hook スニペットを出すことを確認する。
func TestWorktimeRecordPrintPlugin(t *testing.T) {
	// 出力は worktimePluginManifest。event hook の要素を含むこと。
	got := worktimePluginManifest()
	for _, want := range []string{"pane.agent_status_changed", "worktime-record", "[[events]]"} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest スニペットに %q が含まれない:\n%s", want, got)
		}
	}
}
