package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func tm(hhmm string) time.Time {
	t, err := time.Parse(time.RFC3339, "2026-07-02T"+hhmm+":00+09:00")
	if err != nil {
		panic(err)
	}
	return t
}

// TestWorkingIntervals は遷移イベントから working 区間を復元する (working→working は 1 区間、
// 最後が working のままなら openEnd で閉じる)。
func TestWorkingIntervals(t *testing.T) {
	evs := []worktimeEvent{
		{Ts: tm("10:00").Format(time.RFC3339), State: sessWorking},
		{Ts: tm("10:10").Format(time.RFC3339), State: sessWaiting},
		{Ts: tm("10:20").Format(time.RFC3339), State: sessWorking},
		{Ts: tm("10:35").Format(time.RFC3339), State: sessEnded},
		{Ts: tm("10:40").Format(time.RFC3339), State: sessWorking}, // 閉じられず openEnd まで
	}
	got := workingIntervals(evs, tm("10:50"))
	want := []timeInterval{
		{tm("10:00"), tm("10:10")},
		{tm("10:20"), tm("10:35")},
		{tm("10:40"), tm("10:50")},
	}
	if len(got) != len(want) {
		t.Fatalf("区間数 = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || !got[i].End.Equal(want[i].End) {
			t.Errorf("区間[%d] = %v–%v, want %v–%v", i, got[i].Start, got[i].End, want[i].Start, want[i].End)
		}
	}
	if d := sumIntervals(got); d != 10*time.Minute+15*time.Minute+10*time.Minute {
		t.Errorf("合計 = %v, want 35m", d)
	}
}

// working が連続 (working→working) しても 1 区間にまとまる。
func TestWorkingIntervalsDedup(t *testing.T) {
	evs := []worktimeEvent{
		{Ts: tm("10:00").Format(time.RFC3339), State: sessWorking},
		{Ts: tm("10:05").Format(time.RFC3339), State: sessWorking},
		{Ts: tm("10:10").Format(time.RFC3339), State: sessWaiting},
	}
	got := workingIntervals(evs, tm("11:00"))
	if len(got) != 1 || !got[0].Start.Equal(tm("10:00")) || !got[0].End.Equal(tm("10:10")) {
		t.Fatalf("dedup 失敗: %v", got)
	}
}

func TestClipIntervals(t *testing.T) {
	ivs := []timeInterval{
		{tm("10:00"), tm("10:10")},
		{tm("10:20"), tm("10:35")},
		{tm("11:00"), tm("11:30")}, // 窓外 → 消える
	}
	got := clipIntervals(ivs, tm("10:05"), tm("10:30"))
	want := []timeInterval{
		{tm("10:05"), tm("10:10")},
		{tm("10:20"), tm("10:30")},
	}
	if len(got) != len(want) {
		t.Fatalf("clip 区間数 = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || !got[i].End.Equal(want[i].End) {
			t.Errorf("clip[%d] = %v–%v, want %v–%v", i, got[i].Start, got[i].End, want[i].Start, want[i].End)
		}
	}
}

// TestAppendReadWorktimeEvents は追記→読み出しの往復と、壊れた行のスキップを確認する。
func TestAppendReadWorktimeEvents(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	sid := "sess-abc"
	if err := appendWorktimeEvent(sid, sessWorking, tm("10:00")); err != nil {
		t.Fatal(err)
	}
	if err := appendWorktimeEvent(sid, sessWaiting, tm("10:10")); err != nil {
		t.Fatal(err)
	}
	// 壊れた行を混ぜる → 読み出しで飛ばされる。
	p, _ := worktimeLogPath(sid)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("{壊れた行\n")
	f.Close()
	if err := appendWorktimeEvent(sid, sessEnded, tm("10:30")); err != nil {
		t.Fatal(err)
	}
	evs, err := readWorktimeEvents(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("イベント数 = %d, want 3 (壊れ行はスキップ): %v", len(evs), evs)
	}
	if evs[0].State != sessWorking || evs[2].State != sessEnded {
		t.Errorf("順序/内容が想定外: %v", evs)
	}
	// 存在しない session は空。
	if evs, err := readWorktimeEvents("sess-none"); err != nil || len(evs) != 0 {
		t.Errorf("無いログは空のはず: %v err=%v", evs, err)
	}
}

// TestTaskWorktime は end-to-end: link + イベント + タスク窓クリップで working 合計を出す。
func TestTaskWorktime(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	sid := "sess-xyz"
	for _, e := range []struct {
		st string
		at string
	}{
		{sessWorking, "10:00"}, {sessWaiting, "10:10"}, {sessWorking, "10:20"}, {sessEnded, "10:35"},
	} {
		if err := appendWorktimeEvent(sid, e.st, tm(e.at)); err != nil {
			t.Fatal(err)
		}
	}
	// タスク: worktree キー proj--0001 を sid に link。窓 [10:05, 10:30]。
	if err := writeSessionLink("proj--0001", sid, tm("10:00")); err != nil {
		t.Fatal(err)
	}
	task := Task{Project: "proj", ID: "0001", Worktree: "../proj--0001",
		StartedAt: tm("10:05").Format(time.RFC3339), CompletedAt: tm("10:30").Format(time.RFC3339)}
	ivs, total, gotSIDs, ok, err := taskWorktime(task, tm("12:00"))
	if err != nil || !ok {
		t.Fatalf("taskWorktime: ok=%v err=%v", ok, err)
	}
	if len(gotSIDs) != 1 || gotSIDs[0] != sid {
		t.Errorf("session_ids = %v, want [%q]", gotSIDs, sid)
	}
	// [10:00,10:10]→[10:05,10:10]=5m, [10:20,10:35]→[10:20,10:30]=10m。合計 15m。
	if total != 15*time.Minute {
		t.Errorf("working 合計 = %v, want 15m (%v)", total, ivs)
	}
	// link 無しのタスクは ok=false。
	if _, _, _, ok, _ := taskWorktime(Task{Worktree: "../proj--9999"}, tm("12:00")); ok {
		t.Error("link 無しで ok=true")
	}
}

// TestTaskWorktimeMultiSession は中断→別セッション再開で両セッションの working が合算される
// ことを確認する (0102 の主眼)。
func TestTaskWorktimeMultiSession(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	// セッション A: 10:00–10:20 working (中断前)。
	appendWorktimeEvent("sess-A", sessWorking, tm("10:00"))
	appendWorktimeEvent("sess-A", sessEnded, tm("10:20"))
	// セッション B: 14:00–14:30 working (別セッションで再開)。
	appendWorktimeEvent("sess-B", sessWorking, tm("14:00"))
	appendWorktimeEvent("sess-B", sessWaiting, tm("14:30"))

	// 同じタスクを 2 セッションで link (start → 中断 → 別セッションで再 start を模す)。
	writeSessionLink("proj--0001", "sess-A", tm("10:00"))
	writeSessionLink("proj--0001", "sess-B", tm("14:00"))

	task := Task{Project: "proj", ID: "0001", Worktree: "../proj--0001",
		StartedAt: tm("09:00").Format(time.RFC3339), CompletedAt: tm("15:00").Format(time.RFC3339)}
	ivs, total, sids, ok, err := taskWorktime(task, tm("15:00"))
	if err != nil || !ok {
		t.Fatalf("taskWorktime: ok=%v err=%v", ok, err)
	}
	if len(sids) != 2 {
		t.Errorf("session_ids = %v, want 2 件", sids)
	}
	// A=20m + B=30m = 50m。
	if total != 50*time.Minute {
		t.Errorf("合算 working = %v, want 50m (%v)", total, ivs)
	}
	if len(ivs) != 2 {
		t.Errorf("区間数 = %d, want 2 (%v)", len(ivs), ivs)
	}
}

// TestSessionLinkBackwardCompat は旧形式 (単一 session_id、Sessions 無し) の link.json を
// 読めること、再 link で Sessions へ正規化されて履歴が積まれることを確認する。
func TestSessionLinkBackwardCompat(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 旧形式を手で書く (Sessions フィールド無し)。
	old := `{"session_id":"old-sess","updated":"2026-07-02T10:00:00+09:00"}`
	if err := os.WriteFile(filepath.Join(dir, "proj--0001.link.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	l, ok := readSessionLink("proj--0001")
	if !ok || l.SessionID != "old-sess" {
		t.Fatalf("旧形式が読めない: %+v ok=%v", l, ok)
	}
	if ids := linkSessionIDs(l); len(ids) != 1 || ids[0] != "old-sess" {
		t.Errorf("旧形式の linkSessionIDs = %v, want [old-sess]", ids)
	}
	// 再 link で履歴が積まれ、最新が new-sess になる。
	if err := writeSessionLink("proj--0001", "new-sess", tm("14:00")); err != nil {
		t.Fatal(err)
	}
	l2, _ := readSessionLink("proj--0001")
	if l2.SessionID != "new-sess" {
		t.Errorf("最新 = %q, want new-sess", l2.SessionID)
	}
	ids := linkSessionIDs(l2)
	if len(ids) != 2 || !slices.Contains(ids, "old-sess") || !slices.Contains(ids, "new-sess") {
		t.Errorf("履歴 = %v, want [old-sess new-sess]", ids)
	}
	// 同一セッションの再 link は重複しない。
	writeSessionLink("proj--0001", "new-sess", tm("15:00"))
	if ids := linkSessionIDs(mustReadLink(t, "proj--0001")); len(ids) != 2 {
		t.Errorf("重複した: %v", ids)
	}
}

func mustReadLink(t *testing.T, key string) sessionLink {
	t.Helper()
	l, ok := readSessionLink(key)
	if !ok {
		t.Fatalf("link 読めない: %s", key)
	}
	return l
}

// worktime の記録源は herdr プラグインの event hook (worktime-record) に移行した (0114)。
// 遷移のみ追記する挙動のテストは worktime_record_test.go
// (TestWorktimeRecordDedupsConsecutiveSameState) に移した。session-hook はもう worktime を
// 書かない (SESSION 状態のマーカーのみ)。
