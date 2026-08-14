package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubLiveWorking は herdr 生存判定を差し替える (テスト後に復元)。
func stubLiveWorking(t *testing.T, live map[string]bool) {
	t.Helper()
	prev := sessionLiveWorking
	sessionLiveWorking = func(sid string) bool { return live[sid] }
	t.Cleanup(func() { sessionLiveWorking = prev })
}

// stubTranscript は transcript 探索器を差し替え、session_id → 最終活動時刻の表で答えさせる。
// 表に無い session_id は「transcript なし」= cap 経路になる。
func stubTranscript(t *testing.T, last map[string]time.Time) {
	t.Helper()
	dir := t.TempDir()
	for sid, ts := range last {
		path := filepath.Join(dir, sid+".jsonl")
		line := `{"type":"assistant","timestamp":"` + ts.UTC().Format(time.RFC3339) + `"}` + "\n"
		if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prev := transcriptLocators
	transcriptLocators = []func(string) string{
		func(sid string) string {
			path := filepath.Join(dir, sid+".jsonl")
			if _, err := os.Stat(path); err != nil {
				return ""
			}
			return path
		},
	}
	t.Cleanup(func() { transcriptLocators = prev })
}

func openEvents(states []string, at []string) []worktimeEvent {
	evs := make([]worktimeEvent, len(states))
	for i := range states {
		evs[i] = worktimeEvent{Ts: tm(at[i]).Format(time.RFC3339), State: states[i]}
	}
	return evs
}

// TestSessionOpenEndClosedLog は「既に閉じているログ」に何もしないことを確認する
// (この修正が既存の集計を変えない、という一番大事な性質)。
func TestSessionOpenEndClosedLog(t *testing.T) {
	stubLiveWorking(t, nil)
	stubTranscript(t, nil)
	evs := openEvents(
		[]string{sessWorking, sessIdle},
		[]string{"10:00", "10:10"})
	if got := sessionOpenEnd("sess-closed", evs, tm("18:00")); !got.Equal(tm("18:00")) {
		t.Errorf("閉じたログでは winEnd をそのまま返すはず: got %v", got)
	}
}

// TestSessionOpenEndLiveWorking は herdr が「今も working」と言うセッションを打ち切らないことを
// 確認する (長時間の自動実行を過小計上しない)。
func TestSessionOpenEndLiveWorking(t *testing.T) {
	stubLiveWorking(t, map[string]bool{"sess-live": true})
	// transcript の方が古くても、herdr の観測が優先される。
	stubTranscript(t, map[string]time.Time{"sess-live": tm("10:05")})
	evs := openEvents([]string{sessWorking}, []string{"10:00"})
	if got := sessionOpenEnd("sess-live", evs, tm("18:00")); !got.Equal(tm("18:00")) {
		t.Errorf("生存中 working は winEnd まで伸ばすはず: got %v", got)
	}
}

// TestSessionOpenEndTranscriptEvidence は transcript の最終活動時刻で閉じることを確認する。
func TestSessionOpenEndTranscriptEvidence(t *testing.T) {
	stubLiveWorking(t, nil)
	stubTranscript(t, map[string]time.Time{"sess-dead": tm("10:12")})
	evs := openEvents([]string{sessWorking}, []string{"10:00"})
	if got := sessionOpenEnd("sess-dead", evs, tm("18:00")); !got.Equal(tm("10:12")) {
		t.Errorf("transcript 最終活動で閉じるはず: got %v, want 10:12", got)
	}
}

// TestSessionOpenEndSpuriousWorking は「transcript が working より前で終わっている」= 実作業を
// 伴わない空振りの遷移を 0 長に丸めることを確認する (実測 24 本中 21 本がこの形)。
func TestSessionOpenEndSpuriousWorking(t *testing.T) {
	stubLiveWorking(t, nil)
	stubTranscript(t, map[string]time.Time{"sess-spurious": tm("09:30")}) // working より前
	evs := openEvents([]string{sessWorking}, []string{"10:00"})
	got := sessionOpenEnd("sess-spurious", evs, tm("18:00"))
	if !got.Equal(tm("10:00")) {
		t.Fatalf("空振りは開始点に丸めるはず: got %v, want 10:00", got)
	}
	// 0 長なので区間そのものが生まれない。
	if ivs := workingIntervals(evs, got); len(ivs) != 0 {
		t.Errorf("空振りは区間を作らないはず: %v", ivs)
	}
}

// TestSessionOpenEndCapFallback は transcript も herdr も無いときに cap で打ち切ることを確認する。
func TestSessionOpenEndCapFallback(t *testing.T) {
	stubLiveWorking(t, nil)
	stubTranscript(t, nil) // transcript 無し
	evs := openEvents([]string{sessWorking}, []string{"10:00"})
	got := sessionOpenEnd("sess-nolog", evs, tm("18:00"))
	if want := tm("10:00").Add(worktimeOpenCap); !got.Equal(want) {
		t.Errorf("cap で打ち切るはず: got %v, want %v", got, want)
	}
}

// TestSessionOpenEndClampsToWinEnd は cap / transcript が winEnd を超えないことを確認する
// (done タスクの completed_at クリップが従来どおり効く)。
func TestSessionOpenEndClampsToWinEnd(t *testing.T) {
	stubLiveWorking(t, nil)
	t.Run("cap が winEnd を超える", func(t *testing.T) {
		stubTranscript(t, nil)
		evs := openEvents([]string{sessWorking}, []string{"10:00"})
		if got := sessionOpenEnd("sess-a", evs, tm("10:05")); !got.Equal(tm("10:05")) {
			t.Errorf("winEnd で頭打ちのはず: got %v", got)
		}
	})
	t.Run("transcript が winEnd を超える", func(t *testing.T) {
		stubTranscript(t, map[string]time.Time{"sess-b": tm("11:00")})
		evs := openEvents([]string{sessWorking}, []string{"10:00"})
		if got := sessionOpenEnd("sess-b", evs, tm("10:05")); !got.Equal(tm("10:05")) {
			t.Errorf("winEnd で頭打ちのはず: got %v", got)
		}
	})
}

// TestTaskWorktimeUnclosedNoRunaway は退行防止の本丸: 死んだセッションの未クローズ working が
// now まで伸びず、実稼働が有界に収まることを end-to-end で確認する。
func TestTaskWorktimeUnclosedNoRunaway(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	stubLiveWorking(t, nil) // セッションは既に死んでいる
	stubTranscript(t, map[string]time.Time{"sess-dead": tm("10:20")})

	sid := "sess-dead"
	// 10:00–10:10 は正常に閉じた区間。10:15 の working は閉じられないまま。
	for _, e := range []struct{ st, at string }{
		{sessWorking, "10:00"}, {sessIdle, "10:10"}, {sessWorking, "10:15"},
	} {
		if err := appendWorktimeEvent(sid, e.st, tm(e.at)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSessionLink("proj--0016", sid, tm("10:00")); err != nil {
		t.Fatal(err)
	}
	// 未完了タスク (completed_at 無し) = 窓の終端が now。now は 8 時間後。
	task := Task{Project: "proj", ID: "0016", Worktree: "../proj--0016",
		StartedAt: tm("09:00").Format(time.RFC3339)}
	_, total, _, ok, err := taskWorktime(task, tm("18:00"))
	if err != nil || !ok {
		t.Fatalf("taskWorktime: ok=%v err=%v", ok, err)
	}
	// 閉じた区間 10m + 未クローズ分 10:15→10:20 の 5m = 15m。now (18:00) まで伸びない。
	if want := 15 * time.Minute; total != want {
		t.Errorf("実稼働 = %v, want %v (未クローズ区間が now まで伸びている)", total, want)
	}
}

// TestLastTranscriptTimestamp は transcript の末尾走査を確認する: 時刻を持たないメタ行を
// 読み飛ばし、最後の timestamp を拾う。
func TestLastTranscriptTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	body := `{"type":"user","timestamp":"2026-07-02T01:00:00Z"}
{"type":"assistant","timestamp":"2026-07-02T01:05:00Z"}
{"type":"custom-title"}
` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := lastTranscriptTimestamp(path)
	if !ok {
		t.Fatal("timestamp を拾えなかった")
	}
	want, _ := time.Parse(time.RFC3339, "2026-07-02T01:05:00Z")
	if !got.Equal(want) {
		t.Errorf("最終 timestamp = %v, want %v (メタ行を飛ばして遡るはず)", got, want)
	}
	// timestamp を持つ行が 1 つも無ければ ok=false (= cap 経路)。
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, []byte("{\"type\":\"mode\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := lastTranscriptTimestamp(empty); ok {
		t.Error("timestamp 無しは ok=false のはず")
	}
	// 存在しないファイルも ok=false。
	if _, ok := lastTranscriptTimestamp(filepath.Join(dir, "none.jsonl")); ok {
		t.Error("存在しないファイルは ok=false のはず")
	}
}

// TestLastTranscriptTimestampLargeFile は cap を超える transcript でも末尾から拾えること、
// 途中で切った先頭行の壊れた JSON に引きずられないことを確認する。
func TestLastTranscriptTimestampLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// transcriptCapBytes を確実に超えるまで埋める。
	filler := `{"type":"assistant","timestamp":"2026-07-02T00:00:00Z","pad":"` +
		string(make([]byte, 512)) + `"}` + "\n"
	for written := 0; written < transcriptCapBytes+64*1024; written += len(filler) {
		if _, err := f.WriteString(filler); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.WriteString(`{"type":"assistant","timestamp":"2026-07-02T09:00:00Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, ok := lastTranscriptTimestamp(path)
	if !ok {
		t.Fatal("大きい transcript から timestamp を拾えなかった")
	}
	want, _ := time.Parse(time.RFC3339, "2026-07-02T09:00:00Z")
	if !got.Equal(want) {
		t.Errorf("最終 timestamp = %v, want %v", got, want)
	}
}
