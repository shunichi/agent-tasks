package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// --month 既定 (今月) と明示指定で [月初, 翌月初) になる。
func TestResolveReportPeriodMonth(t *testing.T) {
	now := mustParse(t, "2026-07-15T10:00:00+09:00")
	since, until, label, err := resolveReportPeriod(now, true, "", false, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if label != "2026-07" {
		t.Errorf("label = %q, want 2026-07", label)
	}
	if since.Day() != 1 || since.Month() != time.July {
		t.Errorf("since = %v, want 7/1", since)
	}
	if until.Month() != time.August || until.Day() != 1 {
		t.Errorf("until = %v, want 8/1", until)
	}
	// 明示 YYYY-MM
	since2, _, label2, err := resolveReportPeriod(now, true, "2026-03", false, "", "", "")
	if err != nil || label2 != "2026-03" || since2.Month() != time.March {
		t.Errorf("明示 month が反映されない: %v %q", since2, label2)
	}
}

// --week は月曜〜翌月曜。
func TestResolveReportPeriodWeek(t *testing.T) {
	// 2026-07-15 は水曜。その週の月曜は 2026-07-13。
	now := mustParse(t, "2026-07-15T10:00:00+09:00")
	since, until, _, err := resolveReportPeriod(now, false, "", true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if since.Weekday() != time.Monday || since.Day() != 13 {
		t.Errorf("週の開始 = %v, want 月曜 7/13", since)
	}
	if until.Sub(since) != 7*24*time.Hour {
		t.Errorf("週の長さ = %v, want 7d", until.Sub(since))
	}
}

// --since/--until は until 指定日を含む (翌日 0 時が排他境界)。
func TestResolveReportPeriodSinceUntil(t *testing.T) {
	now := mustParse(t, "2026-07-15T10:00:00+09:00")
	since, until, label, err := resolveReportPeriod(now, false, "", false, "", "2026-07-01", "2026-07-10")
	if err != nil {
		t.Fatal(err)
	}
	if since.Day() != 1 || until.Day() != 11 { // 7/10 を含む → 7/11 0時
		t.Errorf("since=%v until=%v, want 7/1〜7/11", since, until)
	}
	if !strings.Contains(label, "2026-07-01") || !strings.Contains(label, "2026-07-10") {
		t.Errorf("label = %q", label)
	}
}

func TestResolveReportPeriodBadInput(t *testing.T) {
	now := mustParse(t, "2026-07-15T10:00:00+09:00")
	if _, _, _, err := resolveReportPeriod(now, true, "2026/07", false, "", "", ""); err == nil {
		t.Error("不正な --month がエラーにならない")
	}
	if _, _, _, err := resolveReportPeriod(now, false, "", false, "", "07-01-2026", ""); err == nil {
		t.Error("不正な --since がエラーにならない")
	}
}

func TestWriteReportMarkdown(t *testing.T) {
	rows := []Task{
		{Project: "webapp", ID: "0001", Title: "A", StartedAt: "2026-07-01T09:00:00+09:00", CompletedAt: "2026-07-01T10:00:00+09:00"},
		{Project: "webapp", ID: "0002", Title: "パイプ|入り", CompletedAt: "2026-07-02T10:00:00+09:00"}, // started_at 欠け
	}
	var buf bytes.Buffer
	writeReport(&buf, rows, "2026-07", false)
	out := buf.String()
	if !strings.Contains(out, "# 完了レポート: 2026-07") {
		t.Error("見出しが無い")
	}
	if !strings.Contains(out, "| 0001 | A | 2026-07-01 09:00 | 2026-07-01 10:00 | 1h |") {
		t.Errorf("行1 の整形が想定外:\n%s", out)
	}
	if !strings.Contains(out, "パイプ\\|入り") {
		t.Error("| がエスケープされていない")
	}
	if !strings.Contains(out, "| - | 2026-07-02 10:00 | - |") {
		t.Errorf("started_at 欠けが - になっていない:\n%s", out)
	}
	if !strings.Contains(out, "合計 2 件") {
		t.Error("合計サマリが無い")
	}
	if !strings.Contains(out, "所要を算出できた 1 件") {
		t.Errorf("所要件数 (started_at 有り 1 件) が想定外:\n%s", out)
	}
}

func TestWriteReportEmpty(t *testing.T) {
	var buf bytes.Buffer
	writeReport(&buf, nil, "2026-08", false)
	if !strings.Contains(buf.String(), "対象期間に完了したタスクはありません") {
		t.Errorf("空メッセージが無い: %q", buf.String())
	}
}

func TestWriteReportCrossProjectSections(t *testing.T) {
	rows := []Task{
		{Project: "alpha", ID: "0001", Title: "a", CompletedAt: "2026-07-01T10:00:00+09:00"},
		{Project: "beta", ID: "0001", Title: "b", CompletedAt: "2026-07-02T10:00:00+09:00"},
	}
	var buf bytes.Buffer
	writeReport(&buf, rows, "2026-07", true)
	out := buf.String()
	if !strings.Contains(out, "## alpha") || !strings.Contains(out, "## beta") {
		t.Errorf("project セクション見出しが無い:\n%s", out)
	}
}
