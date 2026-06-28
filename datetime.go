package main

import (
	"fmt"
	"time"
)

// frontmatter の時刻系フィールド (created / updated / blocked_at) は ISO8601 日時
// (ローカルオフセット込み RFC3339。`date --iso-8601=seconds` 相当) で持つ方針。
// ただし旧データは日付のみ (2006-01-02) のことがあるので、パースは両対応にする。
var timeLayouts = []string{time.RFC3339, "2006-01-02"}

// parseTaskTime は frontmatter の時刻文字列をパースする。空/解析不能なら ok=false。
func parseTaskTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm, true
		}
	}
	return time.Time{}, false
}

// displayDate は日時文字列を一覧表示用に日付 (2006-01-02) へ丸める。
// 内部は日時で持ちつつ、UPDATED 列などは情報過多にならないよう日付だけ見せる。
// パースできなければ元の文字列をそのまま返す (壊さない)。時刻まで見たいときは show で全文を見る。
func displayDate(s string) string {
	tm, ok := parseTaskTime(s)
	if !ok {
		return s
	}
	return tm.Format("2006-01-02")
}

// humanizeSince は now から見た経過を **単一単位** で短く整形する ("3d" / "5h" / "12m" / "now")。
// 一覧の BLOCKED 列のように幅を抑えたい用途向け。解析できなければ "" を返す。
// 所要時間など 2 単位で精密に出したいときは humanizeDuration を使う。
func humanizeSince(since string, now time.Time) string {
	tm, ok := parseTaskTime(since)
	if !ok {
		return ""
	}
	d := now.Sub(tm)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// humanizeDuration は経過/所要時間を **2 単位まで** で整形する ("2d3h" / "1h30m" / "45m" / "30s")。
// 1 分未満は秒、1 時間未満は分、1 日未満は時(分)、それ以上は日(時) まで。負値は 0 扱い。
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m != 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		days := int(d.Hours()) / 24
		if h := int(d.Hours()) % 24; h != 0 {
			return fmt.Sprintf("%dd%dh", days, h)
		}
		return fmt.Sprintf("%dd", days)
	}
}

// leadTime は started_at → completed_at の所要時間を整形する。
// どちらかがパースできなければ "" を返す。
func leadTime(startedAt, completedAt string) string {
	s, okS := parseTaskTime(startedAt)
	c, okC := parseTaskTime(completedAt)
	if !okS || !okC {
		return ""
	}
	return humanizeDuration(c.Sub(s))
}
