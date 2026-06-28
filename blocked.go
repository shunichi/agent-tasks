package main

import "time"

// blocked タスクの可視化。frontmatter の blocked_at (保留にした日時) から経過を出し、
// 長く放置された blocked に気づけるようにする。経過は updated ではなく blocked_at から
// 測る (updated はあらゆる status 更新で上書きされるため)。理由は blocked_reason。
// 時刻のパース/整形は datetime.go の共通ヘルパ (parseTaskTime / humanizeSince) を使う。

// blockedStaleThreshold を超えて放置された blocked は警告色で目立たせる。
const blockedStaleThreshold = 7 * 24 * time.Hour

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
