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

// humanTitlePrefix は human (コードを触らない人手) タスクを一覧で識別するためのプレフィックス。
// 表記は kind の値 (human) と揃える (draft バッジと同じく英語ラベルで統一)。
const humanTitlePrefix = "[human] "

// draftTitlePrefix は簡易登録 (draft) タスクを一覧で識別するためのプレフィックス。TUI から
// タイトルだけで登録された「要件が未整理」の状態を一目で示す (着手前に詳細化する対象)。
// 表記は frontmatter の draft フラグと揃える (英語ラベルで統一)。
const draftTitlePrefix = "[draft] "

// displayTitle は一覧表示 (CLI テーブル / TUI) 用のタイトル装飾を返す。human タスクには識別
// プレフィックスを付け、簡易登録 (draft) には [draft] を付け、blocked タスクには保留理由を添える
// (blockedTitle)。検索 (matchQuery) は生の Title を対象にするので、この装飾はあくまで表示専用
// (検索・JSON には影響しない)。
func displayTitle(t Task) string {
	s := blockedTitle(t)
	if t.IsHuman() {
		s = humanTitlePrefix + s
	}
	if t.Draft {
		s = draftTitlePrefix + s
	}
	return s
}

// BlockedIssue は blocked_at / blocked_reason と status の食い違いを表す doctor の検出結果。
type BlockedIssue struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findBlockedIssues は block の記録/クリア漏れを拾う (記録・クリアは skill 任せなので、
// CLI 側の検査がその防御線になる):
//   - status≠blocked なのに blocked_at / blocked_reason が残っている (start/done でのクリア漏れ)
//   - status=blocked なのに blocked_at が無い (block での記録漏れ。list で経過が "?" になる)
func findBlockedIssues(tasks []Task) []BlockedIssue {
	var out []BlockedIssue
	for _, t := range tasks {
		switch {
		case t.Status != "blocked" && (t.BlockedAt != "" || t.BlockedReason != ""):
			out = append(out, BlockedIssue{t.Project, t.ID, "blocked ではないのに blocked_at/blocked_reason が残っている (クリア漏れ)", t.Path})
		case t.Status == "blocked" && t.BlockedAt == "":
			out = append(out, BlockedIssue{t.Project, t.ID, "status=blocked なのに blocked_at が無い (記録漏れ)", t.Path})
		}
	}
	return out
}
