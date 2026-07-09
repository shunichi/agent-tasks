package main

import (
	"testing"
	"time"
)

// mkIv は指定ローカル時刻の区間を作る (テスト用)。
func mkIv(loc *time.Location, y int, mo time.Month, d, h1, m1, h2, m2 int) timeInterval {
	return timeInterval{
		Start: time.Date(y, mo, d, h1, m1, 0, 0, loc),
		End:   time.Date(y, mo, d, h2, m2, 0, 0, loc),
	}
}

func TestBuildParallelPieces_SingleDay(t *testing.T) {
	loc := time.UTC
	res := []taskWorktimeResult{{
		Project: "webapp", ID: "0001", Title: "t1",
		Intervals: []timeInterval{mkIv(loc, 2026, 7, 1, 9, 30, 10, 45)},
	}}
	got := buildParallelPieces(res)
	if len(got) != 1 {
		t.Fatalf("piece 数 = %d, want 1", len(got))
	}
	p := got[0]
	if p.Date != "2026-07-01" || p.Start != 9*60+30 || p.End != 10*60+45 {
		t.Fatalf("piece = %+v", p)
	}
	if p.Project != "webapp" || p.ID != "0001" || p.Title != "t1" {
		t.Fatalf("identity が保たれていない: %+v", p)
	}
}

func TestBuildParallelPieces_CrossMidnight(t *testing.T) {
	loc := time.UTC
	// 23:30 → 翌 01:15 は 2 日ぶんの piece に割れる。
	iv := timeInterval{
		Start: time.Date(2026, 7, 1, 23, 30, 0, 0, loc),
		End:   time.Date(2026, 7, 2, 1, 15, 0, 0, loc),
	}
	res := []taskWorktimeResult{{Project: "p", ID: "0009", Title: "x", Intervals: []timeInterval{iv}}}
	got := buildParallelPieces(res)
	if len(got) != 2 {
		t.Fatalf("piece 数 = %d, want 2 (%+v)", len(got), got)
	}
	// 出力は日の新しい順。7/2 が先、7/1 が後。
	if got[0].Date != "2026-07-02" || got[0].Start != 0 || got[0].End != 75 {
		t.Fatalf("7/2 の piece が不正: %+v", got[0])
	}
	if got[1].Date != "2026-07-01" || got[1].Start != 23*60+30 || got[1].End != 1440 {
		t.Fatalf("7/1 の piece が不正: %+v", got[1])
	}
}

func TestBuildParallelPieces_SortOrder(t *testing.T) {
	loc := time.UTC
	res := []taskWorktimeResult{
		{Project: "b", ID: "0002", Title: "b", Intervals: []timeInterval{mkIv(loc, 2026, 7, 1, 12, 0, 13, 0)}},
		{Project: "a", ID: "0001", Title: "a", Intervals: []timeInterval{mkIv(loc, 2026, 7, 1, 9, 0, 9, 30)}},
		{Project: "c", ID: "0003", Title: "c", Intervals: []timeInterval{mkIv(loc, 2026, 7, 2, 8, 0, 8, 30)}},
	}
	got := buildParallelPieces(res)
	// 日 desc → 開始分 asc: [7/2 08:00], [7/1 09:00], [7/1 12:00]
	want := []struct {
		date  string
		start int
	}{{"2026-07-02", 480}, {"2026-07-01", 540}, {"2026-07-01", 720}}
	if len(got) != len(want) {
		t.Fatalf("piece 数 = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Date != w.date || got[i].Start != w.start {
			t.Fatalf("piece[%d] = {%s %d}, want {%s %d}", i, got[i].Date, got[i].Start, w.date, w.start)
		}
	}
}

func TestParallelColors_StableWithTimeAllocView(t *testing.T) {
	// 同じ project 集合なら時間配分ビュー (projectColors) と時間帯ビュー (parallelColors) で色が一致する。
	res := []taskWorktimeResult{
		{Project: "webapp", ID: "0001"},
		{Project: "agent-tasks", ID: "0002"},
	}
	pc := parallelColors(res)
	entries := []wtEntry{{Project: "webapp"}, {Project: "agent-tasks"}}
	wc := projectColors(entries)
	for _, name := range []string{"webapp", "agent-tasks"} {
		if pc[name] != wc[name] {
			t.Fatalf("%s の色が不一致: parallel=%s alloc=%s", name, pc[name], wc[name])
		}
	}
}
