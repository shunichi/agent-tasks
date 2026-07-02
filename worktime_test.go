package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	ivs, total, gotSID, ok, err := taskWorktime(task, tm("12:00"))
	if err != nil || !ok {
		t.Fatalf("taskWorktime: ok=%v err=%v", ok, err)
	}
	if gotSID != sid {
		t.Errorf("session_id = %q, want %q", gotSID, sid)
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

// TestSessionHookLogsTransitionsOnly は hook が「状態が変わった時だけ」ログに追記することを確認する。
func TestSessionHookLogsTransitionsOnly(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	cwd := t.TempDir() // 非 git → worktree マーカー経路はスキップ (git 不要)
	sid := "hook-sess"
	runHook := func(event string) {
		in := fmt.Sprintf(`{"hook_event_name":%q,"session_id":%q,"cwd":%q}`, event, sid, cwd)
		tmp := filepath.Join(t.TempDir(), "in.json")
		if err := os.WriteFile(tmp, []byte(in), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(tmp)
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdin
		os.Stdin = f
		defer func() { os.Stdin = old; f.Close() }()
		if err := cmdSessionHook(nil); err != nil {
			t.Fatalf("hook %s: %v", event, err)
		}
	}
	// working, working (変化なし), waiting, waiting (変化なし), working。
	runHook("UserPromptSubmit") // working
	runHook("PreToolUse")       // working (変化なし → 追記しない)
	runHook("PostToolUse")      // working (変化なし)
	runHook("Stop")             // waiting
	runHook("Notification")     // notification_type 無し → state="" → 何もしない
	runHook("SessionEnd")       // ended

	evs, err := readWorktimeEvents(sid)
	if err != nil {
		t.Fatal(err)
	}
	wantStates := []string{sessWorking, sessWaiting, sessEnded}
	if len(evs) != len(wantStates) {
		t.Fatalf("記録された遷移 = %d 件 %v, want %v (変化なしは記録しない)", len(evs), evs, wantStates)
	}
	for i, s := range wantStates {
		if evs[i].State != s {
			t.Errorf("遷移[%d] = %q, want %q", i, evs[i].State, s)
		}
	}
}
