package main

import (
	"fmt"
	"time"
)

// blocked タスクの可視化。frontmatter の blocked_at (保留にした日時) から経過を出し、
// 長く放置された blocked に気づけるようにする。経過は updated ではなく blocked_at から
// 測る (updated はあらゆる status 更新で上書きされるため)。理由は blocked_reason。

// blockedStaleThreshold を超えて放置された blocked は警告色で目立たせる。
const blockedStaleThreshold = 7 * 24 * time.Hour

// timeLayouts は blocked_at / created / updated のパースで試す形式。
// 方針は ISO8601 日時 (RFC3339) だが、日付のみの旧データも読めるよう両対応にする。
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

// humanizeSince は now から見た経過を短く整形する ("3d" / "5h" / "12m" / "now")。
// 解析できなければ "" を返す。
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

// blockedCell は list の BLOCKED 列のセル (保留からの経過) を返す。blocked 以外は空。
// blocked_at が未記録 (旧データ/手動 block) なら "?"。閾値超えは警告色で目立たせる。
func blockedCell(t Task, c colors, now time.Time) cell {
	if t.Status != "blocked" {
		return cell{"", ""}
	}
	tm, ok := parseTaskTime(t.BlockedAt)
	if !ok {
		return cell{"?", c.dim}
	}
	color := c.dim
	if now.Sub(tm) >= blockedStaleThreshold {
		color = c.block // 長期放置 = 要対応。赤で警告
	}
	return cell{humanizeSince(t.BlockedAt, now), color}
}

// blockedTitle は blocked 行の TITLE に理由を添えて返す。理由が無い/blocked 以外は
// title をそのまま返す。理由はテーブルが広がりすぎないよう表示幅で丸める。
func blockedTitle(t Task) string {
	if t.Status != "blocked" || t.BlockedReason == "" {
		return t.Title
	}
	return t.Title + "  (" + truncateDisp(t.BlockedReason, 50) + ")"
}
